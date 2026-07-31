package handler

import (
	"database/sql"
	"errors"
	"teach_me_all/internal/repository"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type CourseHandler struct{
	repo repository.CourseRepository
}


func NewCourseHandler(repo repository.CourseRepository) *CourseHandler{
	return &CourseHandler{repo:repo}
}


func (h *CourseHandler) GetCourseByID(c fiber.Ctx) error{
	id := c.Params("userId")
	
	if _,err := uuid.Parse(id);err!=nil{
		return  fiber.NewError(fiber.StatusBadGateway,"invalid uuid format")
	}

	result,err:=h.repo.GetCourseByID(c.Context(),id)
	if err!=nil{
		if errors.Is(err,sql.ErrNoRows){
			return  fiber.NewError(fiber.StatusNotFound,"course not found")
		}
		return  fiber.NewError(fiber.StatusInternalServerError,"invalid request: "+err.Error())
	}

	return c.JSON(result)
	
}


