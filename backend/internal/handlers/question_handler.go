package handler

import (
	"teach_me_all/internal/repository"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)


type QuestionHandler struct{
	repo repository.Questionrepository
}
func NewQuestionRepository(repo repository.Questionrepository) *QuestionHandler{
	return  &QuestionHandler{repo:repo}
}

func (h *QuestionHandler) GetQuestionByExamID(c fiber.Ctx) error{
	id := c.Params("id")
	if _,err:= uuid.Parse(id);err!=nil{
		return  fiber.NewError(fiber.StatusBadRequest)
	}
	 result,err:=h.repo.GetQuestionByExamsID(c.Context(),id)
	 if err!=nil{
		return err
	 }
	 return  c.JSON(fiber.Map{
		"success":true,
		"data":result,
	 })
}