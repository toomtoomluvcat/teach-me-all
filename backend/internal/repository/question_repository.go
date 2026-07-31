package repository

import (
	"context"
	"teach_me_all/internal/dto"
	"teach_me_all/internal/models"
	apperror "teach_me_all/internal/pkg/errors"

	"gorm.io/gorm"
)


type Questionrepository interface{
	GetQuestionByExamsID(ctx context.Context,id string) (*dto.ExamWithQuestions,error)
}

type questionRepository struct{
	 db *gorm.DB
}

func NewQuestionRepository(db *gorm.DB) Questionrepository {
	return  &questionRepository{db:db}
}

func (r *questionRepository) GetQuestionByExamsID(ctx context.Context,id string) (*dto.ExamWithQuestions,error){
	var exams models.Exam
	if err:=r.db.WithContext(ctx).First(&exams,"id = ?",id).Error;err!=nil{
		return  nil,apperror.MapDBError(err)
	}

	var questions []models.Question
	if err:=r.db.WithContext(ctx).Where("exam_id = ?",id).Find(&questions).Error;err!=nil{
		return  nil,apperror.MapDBError(err)
	}

	
	var questionsResponse []dto.QuestionRespone
	for _,q := range questions{
		questionsResponse = append(questionsResponse,dto.QuestionRespone{
			ID:q.ID,
			Content:q.Content,
			IsCorrect: q.IsCorrect,
		})
	}
	

	if len(questions) == 0{
		questionsResponse = []dto.QuestionRespone{}
	}

	return &dto.ExamWithQuestions{
		ID: exams.ID,
		Title:exams.Title,
		Questions: questionsResponse,
	},nil
}