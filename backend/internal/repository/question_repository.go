package repository

import (
	"context"
	"teach_me_all/internal/dto"
	"teach_me_all/internal/models"
	apperror "teach_me_all/internal/pkg/errors"

	"gorm.io/gorm"
)

type Questionrepository interface{
	GetQuestionsByExamsID(ctx context.Context,id string) (*dto.ExamWithQuestions,error)
}

type questionRepository struct{
	 db *gorm.DB
}

func NewQuestionRepository(db *gorm.DB) Questionrepository {
	return  &questionRepository{db:db}
}

func (r *questionRepository) GetQuestionsByExamsID(ctx context.Context,id string) (*dto.ExamWithQuestions,error){
	var exams dto.ExamResponse
	if err:=r.db.WithContext(ctx).Model(&models.Exam{}).First(&exams,"id = ?",id).Error;err!=nil{
		return  nil,apperror.MapDBError(err)
	}

	var questions []dto.QuestionWithChoice
	if err:=r.db.WithContext(ctx).Model(&models.Question{}).Where("exam_id = ?",id).Find(&questions).Error;err!=nil{
		return  nil,apperror.MapDBError(err)
	}
	for i,q := range questions{
		var choices []dto.ChoiceResponse
		if err:=r.db.WithContext(ctx).Model(&models.Choice{}).Find(&choices,"question_id =  ?",q.ID).Error;err!=nil{
			return  nil,apperror.MapDBError(err)
		}
		questions[i].Choices = choices
	}
	if len(questions) == 0{
		questions = []dto.QuestionWithChoice{}
	}

	return &dto.ExamWithQuestions{
		ID: exams.ID,
		Title:exams.Title,
		Questions: questions,
	},nil
}