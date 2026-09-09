package service

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"strings"

	"qa2a/internal/models"
	"qa2a/internal/repository"

	"github.com/jmoiron/sqlx"
)

// AuthService инкапсулирует бизнес-логику регистрации, авторизации и управления персоналом заведений.
type AuthService struct {
	repo *repository.Repository
}

// NewAuthService создает экземпляр сервиса авторизации.
func NewAuthService(repo *repository.Repository) *AuthService {
	return &AuthService{repo: repo}
}

// AuthResponse возвращает данные авторизованного пользователя и перечень его заведений.
type AuthResponse struct {
	User        *models.User        `json:"user"`
	Memberships []models.Membership `json:"memberships"`
	Token       string              `json:"token"`
}

// LoginOrRegister авторизует пользователя Telegram или регистрирует его при первом входе.
func (s *AuthService) LoginOrRegister(tgID int64, username, fullName string) (*AuthResponse, error) {
	if tgID == 0 {
		return nil, fmt.Errorf("некорректный telegram id пользователя")
	}

	cleanUsername := strings.TrimSpace(username)
	cleanName := strings.TrimSpace(fullName)
	if cleanName == "" {
		cleanName = "Сотрудник"
	}

	user, err := s.repo.CreateUser(tgID, cleanUsername, cleanName)
	if err != nil {
		return nil, fmt.Errorf("ошибка авторизации пользователя: %w", err)
	}

	memberships, err := s.repo.GetMembershipsByUserID(user.ID)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения списка компаний пользователя: %w", err)
	}

	return &AuthResponse{
		User:        user,
		Memberships: memberships,
	}, nil
}

// CreateCompany регистрирует новое заведение, генерирует код доступа, создает базовый склад
// и назначает создателя владельцем (owner).
func (s *AuthService) CreateCompany(ownerID int, name string) (int, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return 0, fmt.Errorf("название заведения не может быть пустым")
	}

	// Генерируем 4-значный инвайт-код с помощью crypto/rand
	n, err := rand.Int(rand.Reader, big.NewInt(9000))
	if err != nil {
		return 0, fmt.Errorf("ошибка генерации кода доступа: %w", err)
	}
	code := fmt.Sprintf("QA-%d", 1000+n.Int64())

	var companyID int

	// Выполняем создание инфраструктуры заведения в транзакции
	err = s.repo.ExecuteInTx(func(tx *sqlx.Tx) error {
		queryComp := `INSERT INTO companies (name, invite_code) VALUES ($1, $2) RETURNING id`
		if err := tx.QueryRow(queryComp, trimmedName, code).Scan(&companyID); err != nil {
			return fmt.Errorf("ошибка вставки компании: %w", err)
		}

		queryLoc := `INSERT INTO locations (company_id, name) VALUES ($1, $2)`
		if _, err := tx.Exec(queryLoc, companyID, "Основной склад"); err != nil {
			return fmt.Errorf("ошибка создания стартового склада: %w", err)
		}

		queryMember := `INSERT INTO memberships (user_id, company_id, role) VALUES ($1, $2, 'owner')`
		if _, err := tx.Exec(queryMember, ownerID, companyID); err != nil {
			return fmt.Errorf("ошибка назначения владельца заведения: %w", err)
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	log.Printf("[auth] 🏢 Создана новая компания #%d ('%s'), инвайт-код: %s, владелец ID: %d", companyID, trimmedName, code, ownerID)
	return companyID, nil
}

// GetUserByTgID находит профиль пользователя по его Telegram ID.
func (s *AuthService) GetUserByTgID(tgID int64) (*models.User, error) {
	return s.repo.GetUserByTgID(tgID)
}

// GetInviteCode возвращает инвайт-код компании, если запрашивающий пользователь состоит в ней.
func (s *AuthService) GetInviteCode(userID, companyID int) (string, error) {
	var code string
	query := `
		SELECT c.invite_code 
		FROM companies c 
		JOIN memberships m ON c.id = m.company_id 
		WHERE m.user_id = $1 AND c.id = $2 
		LIMIT 1`
	err := s.repo.GetInviteCodeRaw(query, userID, companyID, &code)
	if err != nil {
		return "", fmt.Errorf("инвайт-код не найден или у вас нет доступа к заведению: %w", err)
	}
	return code, nil
}

// JoinCompanyByCode подключает пользователя к заведению по инвайт-коду.
func (s *AuthService) JoinCompanyByCode(userID int, code string) (string, error) {
	cleanCode := strings.ToUpper(strings.TrimSpace(code))
	if cleanCode == "" {
		return "", fmt.Errorf("введите код приглашения")
	}
	return s.repo.JoinCompanyByCode(userID, cleanCode)
}

// GetCompanyMembers возвращает список всех участников заведения.
func (s *AuthService) GetCompanyMembers(companyID int) ([]models.MemberInfo, error) {
	return s.repo.GetMembershipsByCompanyID(companyID)
}

// UpdateMemberRole изменяет роль и должность сотрудника с соблюдением иерархии прав доступа.
func (s *AuthService) UpdateMemberRole(companyID, actorID, targetUserID int, role, title string) error {
	actor, err := s.repo.GetMembership(companyID, actorID)
	if err != nil {
		return fmt.Errorf("вы не состоите в этой компании")
	}

	target, err := s.repo.GetMembership(companyID, targetUserID)
	if err != nil {
		return fmt.Errorf("целевой сотрудник не найден в заведении")
	}

	actorRole := strings.ToLower(strings.TrimSpace(actor.Role))
	newRole := strings.ToLower(strings.TrimSpace(role))
	targetRole := strings.ToLower(strings.TrimSpace(target.Role))

	// Только owner, admin и manager имеют доступ к управлению персоналом
	if actorRole != "owner" && actorRole != "admin" && actorRole != "manager" {
		return fmt.Errorf("у вас недостаточно прав для изменения ролей (ваша роль: %s)", actor.Role)
	}

	// Защита владельца: никто, кроме самого владельца, не может менять роль текущего владельца
	if targetRole == "owner" && actorRole != "owner" {
		return fmt.Errorf("только Владелец может изменять настройки своего профиля")
	}

	// Назначить роль 'owner' может только действующий владелец
	if newRole == "owner" && actorRole != "owner" {
		return fmt.Errorf("передать статус Владельца может только текущий Владелец")
	}

	cleanTitle := strings.TrimSpace(title)
	log.Printf("[auth] Изменение роли сотрудника ID:%d -> %s (должность: '%s') инициатором ID:%d", targetUserID, newRole, cleanTitle, actorID)

	return s.repo.UpdateMember(companyID, targetUserID, newRole, cleanTitle)
}

// RemoveMember удаляет сотрудника из заведения с проверкой прав.
func (s *AuthService) RemoveMember(companyID, actorID, targetUserID int) error {
	actor, err := s.repo.GetMembership(companyID, actorID)
	if err != nil {
		return fmt.Errorf("вы не состоите в компании")
	}

	target, err := s.repo.GetMembership(companyID, targetUserID)
	if err != nil {
		return fmt.Errorf("сотрудник не найден в компании")
	}

	actorRole := strings.ToLower(strings.TrimSpace(actor.Role))
	targetRole := strings.ToLower(strings.TrimSpace(target.Role))

	if actorRole != "owner" && actorRole != "admin" && actorRole != "manager" {
		return fmt.Errorf("недостаточно прав для удаления сотрудников")
	}

	// Запрет на удаление владельца
	if targetRole == "owner" {
		return fmt.Errorf("нельзя удалить Владельца заведения")
	}

	// Запрет менеджеру/админу удалять равного или старшего по рангу
	if actorRole != "owner" && (targetRole == "admin" || targetRole == "manager") {
		return fmt.Errorf("удалять администраторов и менеджеров может только Владелец")
	}

	log.Printf("[auth] ❌ Удаление сотрудника ID:%d из компании #%d инициатором ID:%d", targetUserID, companyID, actorID)
	return s.repo.RemoveMember(companyID, targetUserID)
}

