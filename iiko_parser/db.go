package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"time"

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
			initialPass := getEnv("INITIAL_SUPERADMIN_PASSWORD", "")
			if initialPass == "" {
				randomBytes := make([]byte, 16)
				_, _ = rand.Read(randomBytes)
				initialPass = hex.EncodeToString(randomBytes)
				credsContent := fmt.Sprintf("LOGIN=bugh\nPASSWORD=%s\nGENERATED_AT=%s\n", initialPass, time.Now().Format(time.RFC3339))
				credsPath := "/opt/iiko_parser/.superadmin_credentials"
				if errWrite := os.WriteFile(credsPath, []byte(credsContent), 0600); errWrite != nil {
					_ = os.WriteFile(".superadmin_credentials", []byte(credsContent), 0600)
				}
				log.Println("⚠️ INITIAL_SUPERADMIN_PASSWORD не задан. Сгенерирован временный пароль для superadmin 'bugh' и сохранен в .superadmin_credentials (права 0600)")
			}
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
	
	if count < 4 {
		// Очищаем старые системные пресеты
		db.Exec("DELETE FROM parser_prompt_presets WHERE is_default = TRUE")

		_, err = db.Exec(`
			INSERT INTO parser_prompt_presets (id, company_id, name, description, prompt, is_default)
			VALUES 
			(1, 0, 'Автоопределение (Рекомендуется)', 'Автоматическое определение типа документа (ТОРГ-12, УПД, Чек) с помощью Gemini 3.1 Flash Lite.', '', TRUE),
			(2, 0, 'ТОРГ-12 (Товарная накладная ОКУД 0330212)', 'Специализированный парсер ТОРГ-12: извлечение веса из колонки 10 (масса нетто), игнорирование номенклатурных кодов в колонке 3, цена из колонки 11, сумма с НДС из колонки 15.', $1, TRUE),
			(3, 0, 'УПД (Универсальный передаточный документ)', 'Классический парсер УПД и счетов-фактур: количество из колонки 3, цена из колонки 4, сумма из колонки 9.', $2, TRUE),
			(4, 0, 'Товарный чек / Простая квитанция', 'Упрощенный парсер для розничных чеков супермаркетов без кодов ОКЕИ.', $3, TRUE)
			ON CONFLICT (id) DO UPDATE SET 
			    name = EXCLUDED.name, description = EXCLUDED.description, prompt = EXCLUDED.prompt, is_default = TRUE;
			SELECT setval('parser_prompt_presets_id_seq', (SELECT GREATEST(MAX(id), 10) FROM parser_prompt_presets));
		`, torg12ParserPrompt, updParserPrompt, receiptParserPrompt)

		if err != nil {
			log.Printf("⚠️ Ошибка засеивания пресетов: %v", err)
		} else {
			log.Println("✅ Базовые пресеты промптов (Автоопределение, ТОРГ-12, УПД, Чеки) успешно инициализированы")
		}
	} else {
		// Обновляем тексты системных пресетов
		db.Exec("UPDATE parser_prompt_presets SET name = 'Автоопределение (Рекомендуется)', description = 'Автоматическое определение типа документа (ТОРГ-12, УПД, Чек) с помощью Gemini 3.1 Flash Lite.', prompt = '', updated_at = NOW() WHERE id = 1")
		db.Exec("UPDATE parser_prompt_presets SET name = 'ТОРГ-12 (Товарная накладная ОКУД 0330212)', description = 'Специализированный парсер ТОРГ-12: извлечение веса из колонки 10 (масса нетто), игнорирование номенклатурных кодов в колонке 3, цена из колонки 11, сумма с НДС из колонки 15.', prompt = $1, updated_at = NOW() WHERE id = 2", torg12ParserPrompt)
		db.Exec("UPDATE parser_prompt_presets SET name = 'УПД (Универсальный передаточный документ)', description = 'Классический парсер УПД и счетов-фактур: количество из колонки 3, цена из колонки 4, сумма из колонки 9.', prompt = $1, updated_at = NOW() WHERE id = 3", updParserPrompt)
		db.Exec("UPDATE parser_prompt_presets SET name = 'Товарный чек / Простая квитанция', description = 'Упрощенный парсер для розничных чеков супермаркетов без кодов ОКЕИ.', prompt = $1, updated_at = NOW() WHERE id = 4", receiptParserPrompt)
		log.Println("✅ Тексты 4-х системных пресетов актуализированы из констант")
	}
}

func initCompanyInvites() {
	_, err := db.Exec(`
		ALTER TABLE companies ALTER COLUMN invite_code TYPE character varying(64);

		CREATE TABLE IF NOT EXISTS company_invites (
			code VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			accounting_firm_id INT REFERENCES accounting_firms(id) ON DELETE SET NULL,
			expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
			is_used BOOLEAN NOT NULL DEFAULT FALSE,
			used_by_user_id INT REFERENCES users(id) ON DELETE SET NULL,
			company_id INT REFERENCES companies(id) ON DELETE SET NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_company_invites_code ON company_invites (code);
		CREATE INDEX IF NOT EXISTS idx_company_invites_is_used ON company_invites (is_used);
	`)
	if err != nil {
		log.Printf("⚠️ Ошибка инициализации company_invites: %v", err)
	}
}

