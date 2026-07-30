package repository

import (
	"context"
	"teach_me_all/internal/dto"
	"teach_me_all/internal/models"

	"gorm.io/gorm"
)

type CourseRepository interface{
	GetByCourseID(ctx context.Context,id string) (*dto.CourseWithLessons,error)
}

type courseRepository struct{
	db *gorm.DB
}

func NewCourseRepository(db *gorm.DB) CourseRepository {
	return &courseRepository{db: db}
}

func (r *courseRepository) GetByCourseID(ctx context.Context,id string)(*dto.CourseWithLessons,error){
	var course models.Course
	if err := r.db.WithContext(ctx).First(&course,"id = ?",id).Error;err!=nil{ 
		return nil,err
	}

	var lessons []models.Lesson
	if err:= r.db.WithContext(ctx).
	Where("course_id = ?",course.ID).
	Find(&lessons).Error;err!=nil{
		return  nil,err
	}

	if len(lessons ) == 0 {
		return &dto.CourseWithLessons{
			ID:course.ID,
			Title: course.Title,
			IsPublic: course.IsPublic,
			UserID: course.UserID,
			LessonsWithExams: []dto.LessonWithExams{},
		},nil		
	}

	var lessonsWithExams []dto.	LessonWithExams

	for _,l := range lessons{
		var exams []models.Exam
		
		if err:= r.db.WithContext(ctx).
		Where("lesson_id =  ?",l.ID).Find(&exams).Error;err!=nil{
			return  nil,err
		}
		if exams == nil{
			exams = []models.Exam{}
		}

		lessonsWithExams = append(lessonsWithExams,dto.LessonWithExams{
			ID: l.ID,
			Title: l.Title,
			Exams: exams,
		})

	}
	
	return &dto.CourseWithLessons{
		ID:course.ID,
		Title: course.Title,
		IsPublic: course.IsPublic,
		UserID: course.UserID,
		LessonsWithExams: lessonsWithExams,
	},nil

}

