package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

type RoomController struct {
	roomRepo repositories.RoomRepository
}

func NewRoomController(roomRepo repositories.RoomRepository) *RoomController {
	return &RoomController{roomRepo: roomRepo}
}

type CreateRoomRequest struct {
	Name           string `json:"name" binding:"required,min=1,max=100"`
	Capacity       int    `json:"capacity" binding:"required,min=1"`
	Features       string `json:"features,omitempty"`
	Sticky         bool   `json:"sticky"`
	AllowedCourses string `json:"allowed_courses,omitempty"`
}

type UpdateRoomRequest struct {
	Name           *string `json:"name,omitempty" binding:"omitempty,min=1,max=100"`
	Capacity       *int    `json:"capacity,omitempty" binding:"omitempty,min=1"`
	Features       *string `json:"features,omitempty"`
	Sticky         *bool   `json:"sticky,omitempty"`
	AllowedCourses *string `json:"allowed_courses,omitempty"`
}

func (c *RoomController) CreateRoom(ctx *gin.Context) {
	inst := tenantID(ctx)

	var req CreateRoomRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	room := &models.Room{
		Name:     req.Name,
		Capacity: req.Capacity,
		Sticky:   req.Sticky,
		// Stamped from the session, never from the payload.
		InstitutionID: inst,
	}

	if req.Features != "" {
		room.Features = []byte(req.Features)
	}
	if req.AllowedCourses != "" {
		room.AllowedCourses = []byte(req.AllowedCourses)
	}

	if err := c.roomRepo.Create(inst, room); err != nil {
		logger.Error("Failed to create room: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create room"})
		return
	}

	created, _ := c.roomRepo.GetByID(inst, room.ID)
	ctx.JSON(http.StatusCreated, gin.H{"message": "Room created successfully", "room": created})
}

func (c *RoomController) GetRoom(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid room ID"})
		return
	}

	room, err := c.roomRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Room not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"room": room})
}

func (c *RoomController) GetAllRooms(ctx *gin.Context) {
	inst := tenantID(ctx)

	limit, offset := parsePagination(ctx)

	rooms, err := c.roomRepo.GetAll(inst, limit, offset)
	if err != nil {
		logger.Error("Failed to get rooms: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get rooms"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"rooms": rooms, "limit": limit, "offset": offset})
}

func (c *RoomController) UpdateRoom(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid room ID"})
		return
	}

	var req UpdateRoomRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	room, err := c.roomRepo.GetByID(inst, id)
	if err != nil {
		respondRepoError(ctx, "Room not found", err)
		return
	}

	if req.Name != nil {
		room.Name = *req.Name
	}
	if req.Capacity != nil {
		room.Capacity = *req.Capacity
	}
	if req.Features != nil {
		room.Features = []byte(*req.Features)
	}
	if req.Sticky != nil {
		room.Sticky = *req.Sticky
	}
	if req.AllowedCourses != nil {
		room.AllowedCourses = []byte(*req.AllowedCourses)
	}

	if err := c.roomRepo.Update(inst, room); err != nil {
		logger.Error("Failed to update room: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update room"})
		return
	}

	updated, _ := c.roomRepo.GetByID(inst, room.ID)
	ctx.JSON(http.StatusOK, gin.H{"message": "Room updated successfully", "room": updated})
}

func (c *RoomController) DeleteRoom(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid room ID"})
		return
	}

	if err := c.roomRepo.Delete(inst, id); err != nil {
		logger.Error("Failed to delete room: %v", err)
		respondRepoError(ctx, "Room not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Room deleted successfully"})
}
