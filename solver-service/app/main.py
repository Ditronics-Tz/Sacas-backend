"""SACAS timetable CP-SAT solver microservice (OR-Tools)."""

from __future__ import annotations

from typing import List, Optional

from fastapi import FastAPI
from pydantic import BaseModel, Field

from app.solver import solve_timetable

app = FastAPI(title="SACAS Solver Service", version="1.0.0")


class SolverClass(BaseModel):
    id: int
    course_id: int
    number_of_students: int


class SolverModule(BaseModel):
    id: int
    credit_hours: int
    requires_lab: bool = False
    course_id: Optional[int] = None


class SolverSubject(BaseModel):
    id: int
    credit_hours: int


class SolverStaff(BaseModel):
    id: int
    max_hours: int = 40
    module_ids: List[int] = Field(default_factory=list)
    unavailable_days: List[str] = Field(default_factory=list)
    preferred_start: Optional[str] = None


class SolverRoom(BaseModel):
    id: int
    capacity: int
    lab: bool = False
    sticky: bool = False
    course_ids: List[int] = Field(default_factory=list)


class SolverAssignment(BaseModel):
    class_id: int
    module_id: Optional[int] = None
    subject_id: Optional[int] = None
    staff_id: int
    room_id: int
    day: str
    start_time: str
    end_time: str


class SolverRequest(BaseModel):
    class_id: int
    time_budget_sec: float = 30.0
    persist: bool = False
    working_days: List[str] = Field(
        default_factory=lambda: [
            "monday",
            "tuesday",
            "wednesday",
            "thursday",
            "friday",
        ]
    )
    time_slots: List[str] = Field(
        default_factory=lambda: [
            "08:00",
            "09:00",
            "10:00",
            "11:00",
            "13:00",
            "14:00",
            "15:00",
            "16:00",
        ]
    )
    class_: SolverClass = Field(alias="class")
    modules: List[SolverModule] = Field(default_factory=list)
    subjects: List[SolverSubject] = Field(default_factory=list)
    staff: List[SolverStaff] = Field(default_factory=list)
    rooms: List[SolverRoom] = Field(default_factory=list)
    pinned_entries: List[SolverAssignment] = Field(default_factory=list)
    soft_weights: dict = Field(default_factory=dict)

    model_config = {"populate_by_name": True}


class SolverResponse(BaseModel):
    status: str
    assignments: List[SolverAssignment] = Field(default_factory=list)
    violated_soft_constraints: List[str] = Field(default_factory=list)
    unsat_reasons: List[str] = Field(default_factory=list)
    message: Optional[str] = None


@app.get("/health")
def health():
    return {"status": "ok", "service": "solver"}


@app.post("/solve", response_model=SolverResponse)
def solve(req: SolverRequest):
    result = solve_timetable(req.model_dump(by_alias=True))
    return SolverResponse(**result)
