package service

import (
	"fmt"
	"math/rand"
	"qa2a/internal/models"
	"qa2a/internal/repository"
)

type AuthService struct {
	repo *repository.Repository
}

func NewAuthService(repo *repository.Repository) *AuthService {
	return &AuthService{repo: repo}
}

type AuthResponse struct {
	User        *models.User        `json:"user"`
	Memberships []models.Membership `json:"memberships"`
}

func (s *AuthService) LoginOrRegister(tgID int64, username, fullName string) (*AuthResponse, error) {
	user, err := s.repo.CreateUser(tgID, username, fullName)
	if err != nil {
		return nil, err
	}

	memberships, err := s.repo.GetMembershipsByUserID(user.ID)
	if err != nil {
		return nil, err
	}

	return &AuthResponse{User: user, Memberships: memberships}, nil
}

func (s *AuthService) CreateCompany(ownerID int, name string) (int, error) {
	id, err := s.repo.CreateCompany(name)
	if err != nil {
		return 0, err
	}

	code := fmt.Sprintf("QA-%d", 1000+rand.Intn(8999))
	s.repo.SetInviteCode(id, code)
	s.repo.CreateLocation(id, "Основной склад")

	err = s.repo.AddMember(ownerID, id, "owner")
	return id, err
}

func (s *AuthService) GetUserByTgID(tgID int64) (*models.User, error) {
	return s.repo.GetUserByTgID(tgID)
}

func (s *AuthService) GetInviteCode(userID int) (string, error) {
	var code string
	query := `SELECT invite_code FROM companies c 
              JOIN memberships m ON c.id = m.company_id 
              WHERE m.user_id = $1 LIMIT 1`
	err := s.repo.GetInviteCodeRaw(query, userID, &code)
	return code, err
}

func (s *AuthService) JoinCompanyByCode(userID int, code string) (string, error) {
	return s.repo.JoinCompanyByCode(userID, code)
}

func (s *AuthService) GetCompanyMembers(companyID int) ([]models.MemberInfo, error) {
	return s.repo.GetMembershipsByCompanyID(companyID)
}