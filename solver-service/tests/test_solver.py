from app.solver import solve_timetable


def _base_req(**overrides):
    req = {
        "class_id": 1,
        "time_budget_sec": 5,
        "class": {"id": 1, "course_id": 1, "number_of_students": 20},
        "working_days": ["monday", "tuesday", "wednesday", "thursday", "friday"],
        "time_slots": ["08:00", "09:00", "10:00", "11:00", "13:00"],
        "modules": [{"id": 1, "credit_hours": 2, "requires_lab": False, "course_id": 1}],
        "subjects": [],
        "staff": [
            {
                "id": 10,
                "max_hours": 40,
                "module_ids": [1],
                "unavailable_days": [],
                "preferred_start": "08:00",
            },
            {
                "id": 11,
                "max_hours": 40,
                "module_ids": [1],
                "unavailable_days": ["friday"],
                "preferred_start": "09:00",
            },
        ],
        "rooms": [
            {"id": 100, "capacity": 40, "lab": False, "sticky": False, "course_ids": []},
            {"id": 101, "capacity": 30, "lab": True, "sticky": False, "course_ids": []},
        ],
    }
    req.update(overrides)
    return req


def test_feasible_small_instance():
    result = solve_timetable(_base_req())
    assert result["status"] in ("optimal", "feasible")
    assert len(result["assignments"]) == 2
    keys_staff = set()
    keys_room = set()
    keys_class = set()
    for a in result["assignments"]:
        sk = (a["staff_id"], a["day"], a["start_time"])
        rk = (a["room_id"], a["day"], a["start_time"])
        ck = (a["class_id"], a["day"], a["start_time"])
        assert sk not in keys_staff
        assert rk not in keys_room
        assert ck not in keys_class
        keys_staff.add(sk)
        keys_room.add(rk)
        keys_class.add(ck)


def test_infeasible_no_rooms():
    result = solve_timetable(_base_req(rooms=[]))
    assert result["status"] == "infeasible"
    assert result["unsat_reasons"]
    assert result["assignments"] == []


def test_infeasible_capacity():
    req = _base_req()
    req["class"] = {"id": 1, "course_id": 1, "number_of_students": 500}
    result = solve_timetable(req)
    assert result["status"] == "infeasible"
    assert any("capacity" in r.lower() or "500" in r for r in result["unsat_reasons"])


def test_lab_requirement_uses_lab_room():
    req = _base_req()
    req["modules"] = [{"id": 1, "credit_hours": 1, "requires_lab": True, "course_id": 1}]
    result = solve_timetable(req)
    assert result["status"] in ("optimal", "feasible")
    assert len(result["assignments"]) == 1
    assert result["assignments"][0]["room_id"] == 101  # only lab room


def test_lab_infeasible_without_lab_rooms():
    req = _base_req()
    req["modules"] = [{"id": 1, "credit_hours": 1, "requires_lab": True, "course_id": 1}]
    req["rooms"] = [
        {"id": 100, "capacity": 40, "lab": False, "sticky": False, "course_ids": []},
    ]
    result = solve_timetable(req)
    assert result["status"] == "infeasible"
    assert result["assignments"] == []
    assert any("lab" in r.lower() for r in result["unsat_reasons"])
