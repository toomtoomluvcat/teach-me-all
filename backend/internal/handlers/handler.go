package handler

import (
	"teach_me_all/internal/repository"

	"gorm.io/gorm"
)

type Handlers struct{
	Course *CourseHandler
	Question *QuestionHandler
	Exam *ExamHandler
}

func New(db *gorm.DB) *Handlers{
	courseRepo := repository.NewCourseRepository(db)
	questionRepo := repository.NewQuestionRepository(db)
	examRepo := repository.NewExamRepository(db)

	return  &Handlers{
		Course:NewCourseHandler(courseRepo),
	Question: NewQuestionRepository(questionRepo),
	Exam:NewExamRepository(examRepo),
	}
}