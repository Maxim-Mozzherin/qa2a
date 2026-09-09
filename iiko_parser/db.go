package main

import "log"

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

	db.Exec("DELETE FROM parser_prompt_presets WHERE is_default = TRUE AND name != 'Стандартный (УПД / ТОРГ-12)'")

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM parser_prompt_presets WHERE is_default = TRUE").Scan(&count)
	if count == 0 {
		_, err = db.Exec(`
			INSERT INTO parser_prompt_presets (company_id, name, description, prompt, is_default)
			VALUES 
			(0, 'Стандартный (УПД / ТОРГ-12)', 'Основной шаблон для типовых накладных с детальным расчетом фасовок и коэффициентов.', $1, TRUE)
		`, defaultParserPrompt)
		if err != nil {
			log.Printf("⚠️ Ошибка засеивания пресетов: %v", err)
		} else {
			log.Println("✅ Базовые пресеты промптов успешно инициализированы")
		}
	}
}
