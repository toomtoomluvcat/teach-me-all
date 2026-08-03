package repository

import (
	"context"
	"teach_me_all/internal/dto"
	"teach_me_all/internal/models"
	apperror "teach_me_all/internal/pkg/errors"

	"gorm.io/gorm"
)

type CourseRepository interface{
	GetCourseByID(ctx context.Context,id string) (*dto.CourseWithLessons,error)
	GetCoursesByUserID(ctx context.Context,id string)([]dto.CourseResponse,error)

}

type courseRepository struct{
	db *gorm.DB
}

func NewCourseRepository(db *gorm.DB) CourseRepository {
	return &courseRepository{db: db}
}

func (r *courseRepository) CreateCourseByPDF(ctx context.Context,){
	
}

func (r *courseRepository) GetCoursesByUserID(ctx context.Context,id string) ([]dto.CourseResponse,error){
	var courses []models.Course
	if err:=r.db.WithContext(ctx).Find(&courses,"user_id = ?",id).Error;err!=nil{
		return nil,apperror.MapDBError(err)
	}

	coursesResponse :=make([]dto.CourseResponse,0,len(courses))	
	for _,c := range courses{
		coursesResponse = append(coursesResponse, dto.CourseResponse{
			ID: c.ID,
			Title: c.Title,
			IsPublic: c.IsPublic,
		})
	}
	return  coursesResponse,nil
}

func (r *courseRepository) GetCourseByID(ctx context.Context,id string)(*dto.CourseWithLessons,error){
	var course models.Course
	if err := r.db.WithContext(ctx).First(&course,"id = ?",id).Error;err!=nil{ 
		return nil,apperror.MapDBError(err)
	}

	var lessons []models.Lesson
	if err:= r.db.WithContext(ctx).
	Where("course_id = ?",course.ID).
	Find(&lessons).Error;err!=nil{
		return  nil,apperror.MapDBError(err)
	}

	if len(lessons ) == 0 {
		return &dto.CourseWithLessons{
			ID:course.ID,
			Title: course.Title,
			IsPublic: course.IsPublic,
			UserID: course.UserID,
			LessonsWithExams: []dto.LessonWithExam{},
		},nil		
	}

	var lessonsWithExams []dto.LessonWithExam

	for _,l := range lessons{
		var exams []dto.ExamResponse
		
		if err:= r.db.WithContext(ctx).
		Where("lesson_id =  ?",l.ID).Model(&models.Exam{}).Find(&exams).Error;err!=nil{
			return  nil,apperror.MapDBError(err)
		}
		if exams == nil{
			exams = []dto.ExamResponse{}
		}

		lessonsWithExams = append(lessonsWithExams,dto.LessonWithExam{
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

