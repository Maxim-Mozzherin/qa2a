package main

import (
	"log"

	"golang.org/x/crypto/bcrypt"
)

func initRootSuperadmin() {
	// 1. Ensure accounting_firm_id is nullable in accounting_users
	_, err := db.Exec(`
		DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns 
				WHERE table_name = 'accounting_users' 
				  AND column_name = 'accounting_firm_id' 
				  AND is_nullable = 'NO'
			) THEN
				ALTER TABLE accounting_users ALTER COLUMN accounting_firm_id DROP NOT NULL;
			END IF;
		END $$;
	`)
	if err != nil {
		log.Printf("⚠️ Ошибка применения миграции DROP NOT NULL для accounting_firm_id: %v", err)
	}

	// 2. Check if any user with role = 'superadmin' exists in accounting_users
	var superadminCount int
	err = db.QueryRow("SELECT COUNT(*) FROM accounting_users WHERE role = 'superadmin'").Scan(&superadminCount)
	if err != nil {
		log.Printf("⚠️ Ошибка проверки superadmin: %v", err)
	}

	if superadminCount == 0 {
		var bughExists bool
		_ = db.QueryRow("SELECT EXISTS(SELECT 1 FROM accounting_users WHERE login = 'bugh')").Scan(&bughExists)
		if bughExists {
			_, err = db.Exec("UPDATE accounting_users SET role = 'superadmin', is_active = true WHERE login = 'bugh'")
			if err != nil {
				log.Printf("⚠️ Ошибка назначения роли superadmin для bugh: %v", err)
			} else {
				log.Println("✅ Пользователь bugh назначен superadmin платформы")
			}
		} else {
			initialPass := getEnv("INITIAL_SUPERADMIN_PASSWORD", "!123Maxim.!")
			hash, err := bcrypt.GenerateFromPassword([]byte(initialPass), bcrypt.DefaultCost)
			if err != nil {
				log.Printf("⚠️ Ошибка генерации хэша пароля superadmin: %v", err)
			} else {
				_, err = db.Exec(`
					INSERT INTO accounting_users (login, password_hash, role, accounting_firm_id, is_active)
					VALUES ('bugh', $1, 'superadmin', NULL, true)
				`, string(hash))
				if err != nil {
					log.Printf("⚠️ Ошибка создания пользователя bugh: %v", err)
				} else {
					log.Println("✅ Создан первоначальный superadmin 'bugh'")
				}
			}
		}
	}

	// 3. Ensure user bugh has role = 'superadmin' if it exists
	_, err = db.Exec("UPDATE accounting_users SET role = 'superadmin' WHERE login = 'bugh'")
	if err != nil {
		log.Printf("⚠️ Ошибка актуализации роли bugh: %v", err)
	}

	// 4. Ensure user buh is a global_accountant with godmode access but no invite rights
	_, err = db.Exec("UPDATE accounting_users SET role = 'global_accountant', accounting_firm_id = NULL WHERE login = 'buh'")
	if err != nil {
		log.Printf("⚠️ Ошибка обновления роли buh: %v", err)
	}
}

func initPromptPresets() {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS parser_prompt_presets (
			id SERIAL PRIMARY KEY,
			company_id INT NOT NULL DEFAULT 0,
			name VARCHAR(255) NOT NULL,
			description TEXT DEFAULT '',
			prompt TEXT NOT NULL,
			is_default BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_prompt_presets_comp ON parser_prompt_presets(company_id);
	`)
	if err != nil {
		log.Printf("⚠️ Ошибка создания таблицы parser_prompt_presets: %v", err)
		return
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM parser_prompt_presets WHERE is_default = TRUE").Scan(&count)
	
	if count < 2 {
		// Очищаем старые системные пресеты
		db.Exec("DELETE FROM parser_prompt_presets WHERE is_default = TRUE")

		_, err = db.Exec(`
			INSERT INTO parser_prompt_presets (id, company_id, name, description, prompt, is_default)
			VALUES 
			(1, 0, 'Стандартный (УПД / ТОРГ-12)', 'Основной шаблон для типовых накладных и многостраничных документов.', $1, TRUE),
			(2, 0, 'Товарный чек / Простая квитанция', 'Упрощенный парсер для магазинных чеков без кодов ОКЕИ.', $2, TRUE)
			ON CONFLICT (id) DO UPDATE SET 
			    name = EXCLUDED.name, description = EXCLUDED.description, prompt = EXCLUDED.prompt, is_default = TRUE
		`, defaultParserPrompt, receiptParserPrompt)

		if err != nil {
			log.Printf("⚠️ Ошибка засеивания пресетов: %v", err)
		} else {
			log.Println("✅ Базовые пресеты промптов (УПД и Чеки) успешно инициализированы")
		}
	} else {
		// Обновляем только 1 и 2
		db.Exec("UPDATE parser_prompt_presets SET prompt = $1, updated_at = NOW() WHERE id = 1", defaultParserPrompt)
		db.Exec("UPDATE parser_prompt_presets SET prompt = $1, updated_at = NOW() WHERE id = 2", receiptParserPrompt)
		log.Println("✅ Тексты 2-х системных пресетов актуализированы из констант")
	}
}
