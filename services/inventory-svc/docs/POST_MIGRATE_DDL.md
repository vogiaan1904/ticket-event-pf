# Post-migrate DDL

`internal/models/ddl.go` holds the statements applied straight after
`AutoMigrate`, in order. Every one must be idempotent — the service boot path
and the test harness both run them unconditionally on every start.

It is the single source of truth for schema `AutoMigrate` cannot express:
partial indexes and CHECK constraints. Versioned migrations will eventually
replace both this file and `AutoMigrate`.

## Why every constraint is `NOT VALID`

`NOT VALID` enforces the constraint on every new write immediately but skips the
scan of pre-existing rows. A boot that fails on historical drift would take the
service down rather than surface the drift.

Validate deliberately, out of band, once the data is known clean:

```sql
ALTER TABLE ticket_class VALIDATE CONSTRAINT chk_ticket_class_capacity;
```

## The `fk_ticket_class_reservations` repair

**The bug.** The constraint was created from contradictory GORM `OnDelete` tags:
`CASCADE` on `TicketClass.Reservations`, `RESTRICT` on
`Reservation.TicketClass`. `AutoMigrate` creates a foreign key once — the first
time either model is migrated and it does not already exist — and never revisits
it. Whichever side migrated first won (`TicketClass`, from `main.go`'s
`AutoMigrate` call), so every database ever bootstrapped against those tags is
stuck on `CASCADE`. That let `DeleteTicketClass` silently destroy every
reservation referencing it, `CONFIRMED` ones (paid orders) included.

Both struct tags are now `RESTRICT`, but changing a tag does not touch an
already-migrated database. The DDL statement repairs one in place.

**Why it is guarded.** Dropping and re-adding a foreign key takes
`ACCESS EXCLUSIVE` on both tables. On a busy `reservation` table that queues
behind in-flight transactions and then blocks new ones behind it. The DDL only
runs when `confdeltype` is actually wrong:

| `confdeltype` | Meaning | Action |
|---|---|---|
| `r` | RESTRICT — correct | plain catalog SELECT, no lock |
| `c` | CASCADE — the bug | DROP + ADD under lock |
| `a` | NO ACTION | DROP + ADD under lock |

**Why `conrelid` qualifies the lookup.** It restricts the match to the
`reservation` table's own constraint, never a same-named one elsewhere.
`SELECT ... INTO` without `STRICT` would otherwise silently take an arbitrary
matching row.

**Why the re-add is `NOT VALID`.** The rows were under a foreign key the whole
time — only the `ON DELETE` action was wrong — so the data is already
known-valid and a full validating scan under lock would be pure waste.
