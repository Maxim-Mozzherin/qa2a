package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func handlePromptPresets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := GetAuthUser(r)

	switch r.Method {
	case http.MethodGet:
		companyIDStr := r.URL.Query().Get("company_id")
		companyID, _ := strconv.Atoi(companyIDStr)
		if companyID > 0 && !checkAccountantAccessUser(user, companyID) {
			http.Error(w, "Доступ к пресетам заведения запрещен", http.StatusForbidden)
			return
		}

		rows, err := db.Query(`
			SELECT id, company_id, name, description, prompt, is_default, created_at, updated_at
			FROM parser_prompt_presets
			WHERE company_id = 0 OR company_id = $1
			ORDER BY is_default DESC, id ASC
		`, companyID)
		if err != nil {
			http.Error(w, "Ошибка выборки пресетов: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var presets []PromptPreset
		for rows.Next() {
			var p PromptPreset
			if err := rows.Scan(&p.ID, &p.CompanyID, &p.Name, &p.Description, &p.Prompt, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt); err == nil {
				presets = append(presets, p)
			}
		}
		if presets == nil {
			presets = []PromptPreset{}
		}
		json.NewEncoder(w).Encode(presets)

	case http.MethodPost:
		var req struct {
			ID          int    `json:"id"`
			CompanyID   int    `json:"company_id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Prompt      string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Некорректный JSON", http.StatusBadRequest)
			return
		}

		if req.CompanyID > 0 && !checkAccountantAccessUser(user, req.CompanyID) {
			http.Error(w, "Доступ к заведению запрещен", http.StatusForbidden)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.Prompt = strings.TrimSpace(req.Prompt)
		if req.Name == "" || req.Prompt == "" {
			http.Error(w, "Название и текст промпта обязательны", http.StatusBadRequest)
			return
		}

		if req.ID > 0 {
			var isDef bool
			_ = db.QueryRow("SELECT is_default FROM parser_prompt_presets WHERE id = $1", req.ID).Scan(&isDef)
			if isDef {
				var newID int
				err := db.QueryRow(`
					INSERT INTO parser_prompt_presets (company_id, name, description, prompt, is_default)
					VALUES ($1, $2, $3, $4, FALSE)
					RETURNING id
				`, req.CompanyID, req.Name+" (Копия)", req.Description, req.Prompt).Scan(&newID)
				if err != nil {
					http.Error(w, "Ошибка сохранения копии пресета: "+err.Error(), http.StatusInternalServerError)
					return
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": newID, "is_copy": true})
				return
			}

			_, err := db.Exec(`
				UPDATE parser_prompt_presets
				SET name = $1, description = $2, prompt = $3, updated_at = NOW()
				WHERE id = $4 AND is_default = FALSE
			`, req.Name, req.Description, req.Prompt, req.ID)
			if err != nil {
				http.Error(w, "Ошибка обновления пресета: "+err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": req.ID})
		} else {
			var newID int
			err := db.QueryRow(`
				INSERT INTO parser_prompt_presets (company_id, name, description, prompt, is_default)
				VALUES ($1, $2, $3, $4, FALSE)
				RETURNING id
			`, req.CompanyID, req.Name, req.Description, req.Prompt).Scan(&newID)
			if err != nil {
				http.Error(w, "Ошибка создания пресета: "+err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": newID})
		}

	case http.MethodDelete:
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			http.Error(w, "Некорректный ID пресета", http.StatusBadRequest)
			return
		}

		var companyID int
		var isDefault bool
		err = db.QueryRow("SELECT company_id, is_default FROM parser_prompt_presets WHERE id = $1", id).Scan(&companyID, &isDefault)
		if err != nil {
			http.Error(w, "Пресет не найден", http.StatusNotFound)
			return
		}

		if isDefault {
			http.Error(w, "Нельзя удалить системный базовый шаблон", http.StatusBadRequest)
			return
		}

		// Проверяем, имеет ли бухгалтер доступ к заведению, чей пресет удаляет
		if companyID > 0 && !checkAccountantAccessUser(user, companyID) {
			http.Error(w, "Доступ к удалению пресета данного заведения запрещен", http.StatusForbidden)
			return
		}

		res, err := db.Exec("DELETE FROM parser_prompt_presets WHERE id = $1", id)
		if err != nil {
			http.Error(w, "Ошибка удаления: "+err.Error(), http.StatusInternalServerError)
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			http.Error(w, "Пресет не найден", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func handleDefaultPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Только GET метод", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"prompt": defaultParserPrompt,
	})
}
