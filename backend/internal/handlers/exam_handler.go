package handler

import (
	"teach_me_all/internal/repository"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)


type ExamHandler struct{
	repo repository.ExamRepository
}

func NewExamRepository(repo repository.ExamRepository) *ExamHandler{
	return  &ExamHandler{repo:repo}
}

func (h *ExamHandler) GetExamAnswer(c fiber.Ctx) error{
	id := c.Params("id")
	 examId,err := uuid.Parse(id)
	 if err!=nil{
		return  c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":"invalid uuid format",
		})
	}
	result,err := h.repo.GetExamsAnswer(c.Context(),examId)
	if err!=nil{
		return  err
	}
	return  c.JSON(result)
}