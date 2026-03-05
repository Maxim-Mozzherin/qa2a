package service

import (
	"fmt"
	"qa2a/internal/models"
	"qa2a/internal/repository"
)

type AuthService struct {
	repo *repository.Repository
}

func NewAuthService(repo *repository.Repository) *AuthService {
	return &AuthService{repo: repo}
}

func (s *AuthService) LoginOrRegister(tgID int64, username, fullName string) (*models.User, error) {
	// 1. Создаем или берем юзера
	user, err := s.repo.CreateUser(tgID, username, fullName)
	if err != nil {
		return nil, err
	}

	// 2. Проверяем членство (есть ли у него хоть одна компания)
	memberships, err := s.repo.GetMembershipsByUserID(user.ID)
	if err != nil {
		return nil, err
	}

	// 3. Если нет компаний — создаем дефолтную (MVP)
	if len(memberships) == 0 {
		companyID, err := s.repo.CreateCompany(fmt.Sprintf("Бизнес %s", fullName))
		if err != nil {
			return nil, err
		}
		// Привязываем юзера как Owner
		err = s.repo.AddMember(user.ID, companyID, "owner")
		if err != nil {
			return nil, err
		}
	}

	return user, nil
}
