package service

import (
	"fmt"
	"log"        // <--- ДОБАВЛЕНО
	"math/rand"
	"strings"    // <--- ДОБАВЛЕНО
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

func (s *AuthService) GetInviteCode(userID, companyID int) (string, error) {
	var code string
	// Ищем код именно для этой компании, в которой состоит юзер
	query := `SELECT invite_code FROM companies c 
              JOIN memberships m ON c.id = m.company_id 
              WHERE m.user_id = $1 AND c.id = $2 LIMIT 1`
	err := s.repo.GetInviteCodeRaw(query, userID, companyID, &code)
	return code, err
}

func (s *AuthService) JoinCompanyByCode(userID int, code string) (string, error) {
	return s.repo.JoinCompanyByCode(userID, code)
}

func (s *AuthService) GetCompanyMembers(companyID int) ([]models.MemberInfo, error) {
	return s.repo.GetMembershipsByCompanyID(companyID)
}
func (s *AuthService) UpdateMemberRole(companyID, actorID, targetUserID int, role, title string) error {
	actor, err := s.repo.GetMembership(companyID, actorID)
	if err != nil { 
        log.Printf("DEBUG: Ошибка GetMembership: %v", err)
        return fmt.Errorf("вы не состоите в этой компании") 
    }

	log.Printf("DEBUG: Пользователь ID:%d (Имя:%s) пытается изменить роль. Его роль в БД: '%s'", actorID, actor.UserName, actor.Role)

	// Приводим все к нижнему регистру для сравнения
	actorRole := strings.ToLower(actor.Role)
	
	// Разрешаем всем, кроме простого пользователя
	if actorRole == "owner" || actorRole == "manager" || actorRole == "admin" {
		log.Printf("DEBUG: Успешная проверка прав для ID:%d", actorID)
		return s.repo.UpdateMember(companyID, targetUserID, role, title)
	}

	log.Printf("DEBUG: ОТКАЗ в правах для ID:%d, роль: %s", actorID, actor.Role)
	return fmt.Errorf("у вас недостаточно прав (ваша роль: %s)", actor.Role)
}
func (s *AuthService) RemoveMember(companyID, actorID, targetUserID int) error {
    actor, err := s.repo.GetMembership(companyID, actorID)
    if err != nil { return fmt.Errorf("вы не состоите в компании") }

    if actor.Role != "owner" && actor.Role != "manager" && actor.Role != "admin" {
        return fmt.Errorf("недостаточно прав для удаления")
    }
    return s.repo.RemoveMember(companyID, targetUserID)
}