package routes

import (
	handler "teach_me_all/internal/handlers"

	"github.com/gofiber/fiber/v3"
)


func Setup(app *fiber.App,h *handler.Handlers){
	api:=app.Group("/api")
	
	course := api.Group("/courses")
	course.Get("/:id",h.Course.GetCourseByID)

	exam := api.Group("/exams")
	exam.Get("/:id/questions",h.Question.GetQuestionByExamID)

}