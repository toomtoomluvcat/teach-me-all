package handler

import (
	"teach_me_all/internal/repository"

	"gorm.io/gorm"
)

type Handlers struct{
	Course *CourseHandler
}

func New(db *gorm.DB) *Handlers{
	courseRepo := repository.NewCourseRepository(db)

	return  &Handlers{
		Course:NewCourseHandler(courseRepo),
	}
}