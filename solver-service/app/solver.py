"""CP-SAT timetable model."""

from __future__ import annotations

from collections import defaultdict
from typing import Any, Dict, List, Optional, Tuple

from ortools.sat.python import cp_model


def _end_time(start: str) -> str:
    h, m = start.split(":")
    return f"{int(h) + 1:02d}:{m}"


def solve_timetable(req: Dict[str, Any]) -> Dict[str, Any]:
    """
    Build a CP-SAT model and return assignments or unsat reasons.
    """
    class_info = req.get("class") or {}
    class_id = int(req.get("class_id") or class_info.get("id") or 0)
    students = int(class_info.get("number_of_students") or 0)
    course_id = int(class_info.get("course_id") or 0)
    days: List[str] = list(req.get("working_days") or [])
    slots: List[str] = list(req.get("time_slots") or [])
    modules = list(req.get("modules") or [])
    subjects = list(req.get("subjects") or [])
    staff = list(req.get("staff") or [])
    rooms = list(req.get("rooms") or [])
    budget = float(req.get("time_budget_sec") or 30)

    unsat: List[str] = []

    if not days or not slots:
        return {
            "status": "infeasible",
            "assignments": [],
            "violated_soft_constraints": [],
            "unsat_reasons": ["No working days or time slots configured"],
            "message": "invalid domain",
        }

    if not rooms:
        return {
            "status": "infeasible",
            "assignments": [],
            "violated_soft_constraints": [],
            "unsat_reasons": ["No rooms available"],
            "message": "no rooms",
        }

    if not staff:
        return {
            "status": "infeasible",
            "assignments": [],
            "violated_soft_constraints": [],
            "unsat_reasons": ["No staff available"],
            "message": "no staff",
        }

    # Session requirements: (kind, entity_id, session_index, requires_lab)
    requirements: List[Tuple[str, int, int, bool]] = []
    for m in modules:
        ch = int(m.get("credit_hours") or 0)
        for i in range(ch):
            requirements.append(("module", int(m["id"]), i, bool(m.get("requires_lab"))))
    for s in subjects:
        ch = int(s.get("credit_hours") or 0)
        for i in range(ch):
            requirements.append(("subject", int(s["id"]), i, False))

    if not requirements:
        return {
            "status": "optimal",
            "assignments": [],
            "violated_soft_constraints": [],
            "unsat_reasons": [],
            "message": "nothing to schedule",
        }

    # Eligible staff per module (or all staff for subjects / modules without allocation)
    staff_by_module: Dict[int, List[dict]] = defaultdict(list)
    for st in staff:
        for mid in st.get("module_ids") or []:
            staff_by_module[int(mid)].append(st)

    def eligible_staff(kind: str, eid: int) -> List[dict]:
        if kind == "module" and staff_by_module.get(eid):
            return staff_by_module[eid]
        return staff

    capacious_rooms = [r for r in rooms if int(r.get("capacity") or 0) >= students]
    if not capacious_rooms:
        unsat.append(
            f"Class {class_id} needs capacity >= {students} but no room is large enough"
        )
        return {
            "status": "infeasible",
            "assignments": [],
            "violated_soft_constraints": [],
            "unsat_reasons": unsat,
            "message": "capacity",
        }

    model = cp_model.CpModel()

    # Decision vars: (req_idx, day_i, slot_i, staff_i, room_i) -> BoolVar
    x: Dict[Tuple[int, int, int, int, int], cp_model.IntVar] = {}
    # Also track by resource for constraints
    by_req: Dict[int, List[cp_model.IntVar]] = defaultdict(list)
    # class is single — no two sessions same day/slot
    by_class_slot: Dict[Tuple[int, int], List[cp_model.IntVar]] = defaultdict(list)
    by_staff_slot: Dict[Tuple[int, int, int], List[cp_model.IntVar]] = defaultdict(list)
    by_room_slot: Dict[Tuple[int, int, int], List[cp_model.IntVar]] = defaultdict(list)
    # staff hours count
    by_staff_vars: Dict[int, List[cp_model.IntVar]] = defaultdict(list)

    soft_penalty_vars: List[Tuple[cp_model.IntVar, int, str]] = []

    for ri, (kind, eid, _si, requires_lab) in enumerate(requirements):
        estaff = eligible_staff(kind, eid)
        eroms = [
            r
            for r in capacious_rooms
            if (not requires_lab) or bool(r.get("lab"))
        ]
        if requires_lab and not eroms:
            unsat.append(
                f"Session {kind}={eid} requires a lab room but no capacious lab rooms exist"
            )
            continue

        candidates = 0
        for di, day in enumerate(days):
            for si, start in enumerate(slots):
                for st in estaff:
                    unavail = {d.lower() for d in (st.get("unavailable_days") or [])}
                    if day.lower() in unavail:
                        continue
                    for room in eroms:
                        # room course affinity not hard
                        var = model.NewBoolVar(f"x_{ri}_{di}_{si}_{st['id']}_{room['id']}")
                        key = (ri, di, si, int(st["id"]), int(room["id"]))
                        x[key] = var
                        by_req[ri].append(var)
                        by_class_slot[(di, si)].append(var)
                        by_staff_slot[(int(st["id"]), di, si)].append(var)
                        by_room_slot[(int(room["id"]), di, si)].append(var)
                        by_staff_vars[int(st["id"])].append(var)
                        candidates += 1

                        preferred = (st.get("preferred_start") or "").strip()
                        if preferred and preferred != start:
                            # soft: prefer preferred_start
                            soft_penalty_vars.append((var, 1, f"staff {st['id']} not at preferred_start"))

        if candidates == 0:
            unsat.append(
                f"Session requirement {kind}={eid} has zero feasible (day,slot,staff,room) candidates"
            )
        else:
            model.Add(sum(by_req[ri]) == 1)

    if unsat:
        return {
            "status": "infeasible",
            "assignments": [],
            "violated_soft_constraints": [],
            "unsat_reasons": unsat,
            "message": "no candidates",
        }

    # Hard: no class double-book
    for key, vars_ in by_class_slot.items():
        if len(vars_) > 1:
            model.Add(sum(vars_) <= 1)

    # Hard: no staff double-book
    for key, vars_ in by_staff_slot.items():
        if len(vars_) > 1:
            model.Add(sum(vars_) <= 1)

    # Hard: no room double-book
    for key, vars_ in by_room_slot.items():
        if len(vars_) > 1:
            model.Add(sum(vars_) <= 1)

    # Hard: staff max hours (each session = 1 hour)
    for st in staff:
        sid = int(st["id"])
        max_h = int(st.get("max_hours") or 40)
        vars_ = by_staff_vars.get(sid) or []
        if vars_:
            model.Add(sum(vars_) <= max_h)

    # Soft: spread sessions across days (penalize same-day clustering for same class)
    # Count sessions per day; encourage balance via squared deviation approx with linear penalties
    day_counts = []
    for di, _day in enumerate(days):
        day_vars = []
        for si in range(len(slots)):
            day_vars.extend(by_class_slot.get((di, si), []))
        if day_vars:
            cnt = model.NewIntVar(0, len(requirements), f"day_count_{di}")
            model.Add(cnt == sum(day_vars))
            day_counts.append(cnt)
            # soft: penalize counts above 2
            over = model.NewIntVar(0, len(requirements), f"day_over_{di}")
            model.Add(over >= cnt - 2)
            model.Add(over >= 0)
            soft_penalty_vars.append((over, 3, f"class sessions clustered on {days[di]}"))

    # Objective
    objective_terms = []
    for var_or_int, weight, _msg in soft_penalty_vars:
        objective_terms.append(weight * var_or_int)
    if objective_terms:
        model.Minimize(sum(objective_terms))

    solver = cp_model.CpSolver()
    solver.parameters.max_time_in_seconds = budget
    status = solver.Solve(model)

    if status not in (cp_model.OPTIMAL, cp_model.FEASIBLE):
        return {
            "status": "infeasible",
            "assignments": [],
            "violated_soft_constraints": [],
            "unsat_reasons": [
                f"Class {class_id} has no feasible timetable under hard constraints "
                f"(sessions={len(requirements)}, staff={len(staff)}, rooms={len(rooms)})"
            ],
            "message": "cp-sat unsat",
        }

    assignments = []
    violated = []
    for (ri, di, si, sid, rid), var in x.items():
        if solver.Value(var) == 1:
            kind, eid, _si, _lab = requirements[ri]
            start = slots[si]
            a = {
                "class_id": class_id,
                "staff_id": sid,
                "room_id": rid,
                "day": days[di],
                "start_time": start,
                "end_time": _end_time(start),
            }
            if kind == "module":
                a["module_id"] = eid
                a["subject_id"] = None
            else:
                a["module_id"] = None
                a["subject_id"] = eid
            assignments.append(a)

            # soft notes for preferred start
            for st in staff:
                if int(st["id"]) == sid:
                    pref = (st.get("preferred_start") or "").strip()
                    if pref and pref != start:
                        violated.append(
                            f"Staff {sid} scheduled at {start} (preferred {pref}) on {days[di]}"
                        )
                    break

    # Deduplicate soft notes
    violated = sorted(set(violated))

    status_str = "optimal" if status == cp_model.OPTIMAL else "feasible"
    return {
        "status": status_str,
        "assignments": assignments,
        "violated_soft_constraints": violated,
        "unsat_reasons": [],
        "message": f"solved ({status_str})",
    }
