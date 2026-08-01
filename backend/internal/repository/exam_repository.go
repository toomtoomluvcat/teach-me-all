package repository

import (
	"context"
	"teach_me_all/internal/dto"
	"teach_me_all/internal/models"
	apperror "teach_me_all/internal/pkg/errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ExamRepository interface{
	GetExamsAnswer(ctx context.Context,id uuid.UUID)(*dto.ExamAnswers,error)
}

type examRepository struct{
	db *gorm.DB
}

func NewExamRepository(db *gorm.DB) ExamRepository{
	return  &examRepository{db:db}
}

func (r* examRepository) GetExamsAnswer (ctx context.Context,id uuid.UUID)(*dto.ExamAnswers,error){
	var questions []models.Question
	if err:=r.db.WithContext(ctx).
	Find(&questions,"exam_id = ?",id).Error;err!=nil{
		return nil,apperror.MapDBError(err)
	}

	err := r.db.WithContext(ctx).Model(&models.Exam{}).
	Where("id = ?",id).
	Update("has_taken",true).Error;
	
	if err!=nil{
		return nil,apperror.MapDBError(err)
	}

	questionAnswers := make([]dto.QuestionAnswer,0,len(questions))
	for _,q := range questions{
		var choice models.Choice
		if err:= r.db.WithContext(ctx).Model(&models.Choice{}).
		Where("is_correct = true AND question_id = ?",q.ID).
		First(&choice).Error;err!=nil{
			return  nil,apperror.MapDBError(err)
		}
		questionAnswers = append(questionAnswers, dto.QuestionAnswer{
			ID:q.ID,
			AnswerID: choice.ID,
		})
	}

	return  &dto.ExamAnswers{
		ID:id,
		QuestionAnswers: questionAnswers,
	},nil

}