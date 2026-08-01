package routes

import (
	"teach_me_all/internal/config"
	handler "teach_me_all/internal/handlers"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
)


func Setup(app *fiber.App,h *handler.Handlers){
	app.Use(cors.New(config.CorsConfig()))
	app.Use(logger.New())

	api:=app.Group("/api")

	user := api.Group("/user")
	user.Get("/:id/courses",h.Course.GetCoursesByUserID)

	course := api.Group("/courses")
	course.Get("/:id",h.Course.GetCourseByID)

	exam := api.Group("/exams")
	exam.Get("/:id/questions",h.Question.GetQuestionByExamID)
	exam.Get("/:id/answers",h.Exam.GetExamAnswer)

}