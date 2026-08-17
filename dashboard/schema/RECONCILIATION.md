# Schema reconciliation — ArcadeDB 26.7.2

`T-001` asks for the DDL to be applied to a scratch ArcadeDB on the target
version and for the dialect to be reconciled. This is that reconciliation.

- **Target version:** ArcadeDB **26.7.2**, pinned by `D-043`. Run as
  `arcadedata/arcadedb:26.7.2`.
- **Source of truth:** `artifacts/v1/gaos-arcadedb-model.html` §05–§10, from
  which the ten `.sql` files in this directory were extracted. Seven are
  verbatim and say so in their header; `08-edges.sql`, `09-indexes.sql` and
  `10-storage.sql` carry the reconciliations below and say that instead.
- **Applied by:** `deploy/schema/apply.sh` from a workstation, and — since
  `T-007` landed — by the dashboard binary from an embedded copy
  (`dashboard/schema_embed.go`), which is why the SQL lives inside the
  `dashboard` module rather than beside the script.
- **Date measured:** 2026-08-16; deviation 3 added 2026-08-17.
- **Result:** 321 statements apply clean, and re-applying is a no-op. **Three**
  deviations from the committed model document were required to get there: one
  is dialect, **one is a design error in the model**, and one is an
  incompleteness found later, when `T-006` first wrote to the graph.

The model document warns in its own words that the ArcadeDB SQL dialect moves
between releases and that the schema should be run against the target version
in a scratch database first. It was right to.

---

## Deviation 1 — `UNIQUE` is not a `CREATE EDGE TYPE` modifier

**Severity:** dialect. **Statements affected:** 32 (plus 18 cascading failures).

The model declares every edge type as:

```sql
CREATE EDGE TYPE OWNS IF NOT EXISTS EXTENDS GaosEdge UNIQUE;
```

26.7.2 rejects this outright:

```
SQL syntax error at line 1, column 53: extraneous input 'UNIQUE'
expecting {<EOF>, ';'}
```

All 32 edge-type declarations failed, and with them the 18 `CREATE PROPERTY`
statements that referenced a type which was therefore never created — which is
why the first run reported 53 failures rather than 32.

**The constraint itself is supported**; only that spelling of it is not. What
the model's edge-catalog `unique` column means — *at most one edge per ordered
pair* — is expressed as a unique index over the two endpoints:

```sql
CREATE EDGE TYPE OWNS IF NOT EXISTS EXTENDS GaosEdge;
CREATE INDEX IF NOT EXISTS ON OWNS (`@out`, `@in`) UNIQUE;
```

**Applied:** the `UNIQUE` keyword was removed from all 32 declarations in
`08-edges.sql`, and 32 endpoint indexes were appended to `09-indexes.sql` under
their own heading. Every edge type in the catalog is marked unique, so all 32
carry one. `CALLS` is the one worth a second look and it is correct: repeated
calls increment `callCount` on the single edge rather than adding a second.

**Ruled `D-045`, 2026-08-16.** Additive, no semantics change. The model
document is amended in place: §04's edge-catalog lede now describes the index
form, and §09 carries an amendment callout above the DDL block, which is left as
first written so the amendment stays legible.

---

## Deviation 2 — `partitioned('tenantId')` cannot mean what the model wants

**Severity:** design error in the committed model. **Statements affected:** 3.
**This one is not cosmetic and needs a ruling.**

`10-storage.sql` (model §13) asks for:

```sql
ALTER TYPE Task    BucketSelectionStrategy `partitioned('tenantId')`;
ALTER TYPE TaskRun BucketSelectionStrategy `partitioned('tenantId')`;
ALTER TYPE Event   BucketSelectionStrategy `partitioned('tenantId')`;
```

with the stated intent: *"Route records to buckets by tenant so one tenant's
write burst does not contend with another's, and so a per-tenant export is a
bucket-local scan."*

26.7.2 answers:

```
Bucket selection strategy implementation 'partitioned('tenantId')' was not
found -> Cannot find a unique index on properties [tenantId]
```

The syntax is fine — `ALTER TYPE … BucketSelectionStrategy` is accepted, and
`round-robin` sets successfully. The strategy specifically requires a **unique
index on exactly `(tenantId)`**. Supplying one makes the `ALTER` succeed:

```sql
CREATE INDEX ON Task (tenantId) UNIQUE;                          -- succeeds
ALTER TYPE Task BucketSelectionStrategy `partitioned('tenantId')`;  -- succeeds
```

and then breaks the database, which was measured rather than reasoned about:

```sql
INSERT INTO Task SET id='t-1', tenantId='loop-eng', … ;   -- ok
INSERT INTO Task SET id='t-2', tenantId='loop-eng', … ;
-- DuplicatedKeyException: Duplicated key [loop-eng] found on index
-- 'Task[tenantId]' already assigned to record #98:0
```

**A unique index on `tenantId` permits one Task per tenant.** ArcadeDB's
`partitioned` strategy exists to partition by a record's *primary key* so that
a lookup by that key goes straight to one bucket. `tenantId` is a discriminator
shared by every record of a tenant, which is the opposite of a primary key. The
model asks the strategy to do something it is not for.

**Applied:** the three statements are commented out in `10-storage.sql` with the
reason inline. `T-004`'s other half stands and is met by `09-indexes.sql`:

| Index | Kind |
|---|---|
| `Task (tenantId, id)` | UNIQUE |
| `TaskRun (tenantId, id)` | UNIQUE |
| `Event (tenantId, seq)` | UNIQUE |
| `Task (tenantId, status, priority)` | NOTUNIQUE |

`BR-26` requires `tenantId` "declared and indexed whatever the tenancy strategy
turns out to be", and that is satisfied. Isolation rests where `D-042` puts it
— **database-per-tenant as the default**, with `tenantId` as the second line
rather than the first — which is also what `T-136` (a tenant sees nothing of
another even when a query forgets to filter) actually depends on.

**Ruled `D-046`, 2026-08-16 — the withdrawal is accepted.** Under
database-per-tenant, per-tenant bucket partitioning inside a shared type solves a
problem the deployment default does not have. §13 is amended in place with the
three statements commented and the reasoning above them, so a future reader meets
the correction where they would otherwise copy the mistake.

---

## Deviation 3 — an identity index on seven types, but every type is addressed by id

**Severity:** incompleteness in the committed model. **Statements affected:** 7
added. **Found on 2026-08-17, writing `store_arcade.go` (`T-006`).**

The model gives `(tenantId, id) UNIQUE` to seven vertex types: `Product`,
`Workflow`, `Task`, `TaskRun`, `Agent`, `Issue`, `Knowledge`. The other eighteen
have none.

That is fine until something writes them. `BR-03` says every reference is a
business `id` and never a RID, so a write is keyed by `(tenantId, id)` — and
26.7.2 refuses a keyed write against a type with no index for it:

```
Upsert must involve an index to retrieve the records. Check the where
condition is using the index for the upsert
```

Every `UPSERT` in `store_arcade.go` failed this way on the first run, against
`Tenant`, `Room`, `Requirement`, `AcceptanceCriterion`, `Artifact`, `Decision`
and `Repository`.

**Applied:** those seven indexes are appended to `09-indexes.sql` under their own
heading. The remaining eleven types are deliberately left without one — nothing
writes them yet, and the model is explicit that an index belongs where a lookup
is actually hot rather than everywhere one might eventually be. The next task
that writes one of them adds its index in the same change.

**Ruled `D-047`, 2026-08-17.** Additive, no semantics change: it makes explicit
a uniqueness the model already assumed by addressing records by business id.
The DDL now applies **321** statements rather than 314.

---

## Everything else applied as written

`09-indexes.sql`'s `BY ITEM`, `FULL_TEXT` and composite forms all applied
without complaint on 26.7.2. The commented-out `LSM_VECTOR` block was left
commented, as the model leaves it.

---

## What was confirmed working, and is worth knowing

- **`GET /api/v1/ready` answers 204, not 200.** `dashboard/arcade.go` measured
  this on 2026-08-07 and accepts both; still true on 26.7.2.
- **Property constraints are enforced by the engine.** `status='NONSENSE'` on
  `Task` is refused with `ValidationException`. The `Task.status` regexp
  (`PENDING|READY|DISPATCHED|RUNNING|REVIEW|BLOCKED|DONE|FAILED|CANCELED`) is a
  real boundary, not documentation — which matters for `M0.5`: the state
  machine's *vocabulary* is already enforced; what `T-017` still owes is the
  *transition* rules between those values.
- **`EXTERNAL` applied cleanly** on `ContextSlice.rendered`, `Prompt.template`
  and `Artifact.body`, so `BR-25` is available as specified.
- **Idempotence holds.** Second run: 0 failures, 194 statements reported as
  already present. Re-measured on 2026-08-17 after `D-047`: still 0 failures.
- **`ifnull(createdAt, sysdate())` is how an UPSERT keeps an audit stamp still.**
  `createdAt` is mandatory on `GaosVertex`, an UPSERT cannot branch on whether it
  inserted, and setting it in a follow-up `UPDATE` fails — the mandatory-property
  check runs at commit and the record is already invalid by then.
- **`retry` is a reserved word.** `UPDATE Workflow SET retry = :p` is a syntax
  error on 26.7.2. `store_arcade.go` writes `retryCount` and `escalationCount`,
  which is also the vocabulary `Task` already declares.
- **There is no `CREATE EDGE … IF NOT EXISTS`,** and inside a transaction the
  endpoint-index violation is raised at COMMIT rather than at the statement — so
  catching `DuplicatedKeyException` per statement never fires. Edge writes check
  for the edge first, by traversal.

---

## Reproducing this

```bash
docker run -d --rm --name gaos-scratch-db -p 2480:2480 \
  -e JAVA_OPTS="-Darcadedb.server.rootPassword=scratchpw" \
  arcadedata/arcadedb:26.7.2

GAOS_ARCADE_USER=root GAOS_ARCADE_PASSWORD=scratchpw \
  ./deploy/schema/apply.sh scratch      # 321 applied, 0 failed
GAOS_ARCADE_USER=root GAOS_ARCADE_PASSWORD=scratchpw \
  ./deploy/schema/apply.sh scratch      # 0 failed, idempotent

docker stop gaos-scratch-db
```

Note `--rm` and no volume: the container is deliberately throwaway. Nothing in
this directory should ever be applied first to an instance anyone depends on —
`dev` and `prod` are shared ArcadeDB instances belonging to the wider
`master-compose-ai*` stacks, and GAOS is a guest there.
