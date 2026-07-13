# Solver service contract

**Service:** `solver-service` (Python FastAPI + Google OR-Tools CP-SAT)  
**Internal URL:** `http://solver:8090` (compose) / `http://localhost:8090` (local)  
**Not internet-exposed** in production (bind to internal network only).

## Endpoints

### `GET /health`

```json
{ "status": "ok", "service": "solver" }
```

### `POST /solve`

Request body (snake_case):

```json
{
  "class_id": 1,
  "time_budget_sec": 30,
  "persist": false,
  "working_days": ["monday", "tuesday", "wednesday", "thursday", "friday"],
  "time_slots": ["08:00", "09:00", "10:00", "11:00", "13:00", "14:00", "15:00", "16:00"],
  "class": {
    "id": 1,
    "course_id": 10,
    "number_of_students": 45
  },
  "modules": [
    { "id": 2, "credit_hours": 3, "requires_lab": true, "course_id": 10 }
  ],
  "subjects": [
    { "id": 5, "credit_hours": 1 }
  ],
  "staff": [
    {
      "id": 7,
      "max_hours": 40,
      "module_ids": [2],
      "unavailable_days": ["saturday"],
      "preferred_start": "08:00"
    }
  ],
  "rooms": [
    {
      "id": 3,
      "capacity": 50,
      "lab": true,
      "sticky": false,
      "course_ids": []
    }
  ],
  "pinned_entries": [],
  "soft_weights": {}
}
```

Response:

```json
{
  "status": "optimal",
  "assignments": [
    {
      "class_id": 1,
      "module_id": 2,
      "subject_id": null,
      "staff_id": 7,
      "room_id": 3,
      "day": "monday",
      "start_time": "08:00",
      "end_time": "09:00"
    }
  ],
  "violated_soft_constraints": [
    "Staff 7 scheduled at 10:00 (preferred 08:00) on tuesday"
  ],
  "unsat_reasons": [],
  "message": "solved (optimal)"
}
```

### Status values

| status | meaning |
|--------|---------|
| `optimal` | CP-SAT proved optimality within time budget |
| `feasible` | Feasible solution found (may not be proven optimal) |
| `infeasible` | No solution under hard constraints — see `unsat_reasons` |
| `error` | Solver internal failure |

### Hard constraints enforced

- Exactly one slot assignment per required session (`credit_hours` sessions per module/subject)
- No class / staff / room double-booking in the same day+start
- Room capacity ≥ class size
- Prefer lab rooms when `requires_lab` (falls back with soft note if none)
- Staff `unavailable_days` respected
- Staff weekly hours ≤ `max_hours`

### Soft objectives

- Prefer staff `preferred_start`
- Prefer spreading class sessions across days (penalize >2 sessions/day)

### Curriculum source of truth (Go client)

To avoid double-counting **general subjects**:

1. Always include course modules for the class’s `course_id`.
2. Always include modules with `type=general_subject` / null `course_id`.
3. Include `subjects` table rows **only if** there are no `general_subject` modules (legacy data).

Lab requirement is a **hard** constraint: if `requires_lab` and no capacious lab room exists, status is `infeasible`.

### Go integration

- Client: `Sacas-backend/internal/services/solverclient.go`
- `POST /api/protected/timetable/generate` and `/generate/preview` call the solver when `SOLVER_URL` is set
- On solver failure with `SOLVER_FALLBACK=true`, greedy engine is used
- Infeasible → HTTP `422` with `unsat_reasons`
