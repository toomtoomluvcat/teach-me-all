package handler

import (
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
	id := c.Params("id")
	
	if _,err := uuid.Parse(id);err!=nil{
		return  fiber.NewError(fiber.StatusBadRequest)
		}

	result,err:=h.repo.GetCourseByID(c.Context(),id)
	if err!=nil{
		return  err
	}

	return c.JSON(fiber.Map{
		"success":true,
		"data":result,
	})
	
}


