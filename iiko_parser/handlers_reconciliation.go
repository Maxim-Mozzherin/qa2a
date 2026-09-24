package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ReconciliationMatchItem struct {
	DocNumber string  `json:"doc_number"`
	DocDate   string  `json:"doc_date"`
	ActAmount float64 `json:"act_amount"`
	DbAmount  float64 `json:"db_amount"`
	Diff      float64 `json:"diff"`
	Status    string  `json:"status"`
}

type ReconciliationResponseData struct {
	Matched     []ReconciliationMatchItem `json:"matched"`
	Mismatched  []ReconciliationMatchItem `json:"mismatched"`
	MissingInDb []ReconciliationMatchItem `json:"missing_in_db"`
	PhantomInDb []ReconciliationMatchItem `json:"phantom_in_db"`
	Stats       map[string]interface{}    `json:"stats"`
}

func handleParseReconciliation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST метод", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(100 << 20); err != nil {
		http.Error(w, "Слишком большой файл (макс 100 MB)", http.StatusBadRequest)
		return
	}

	companyIDStr := r.FormValue("company_id")
	if companyIDStr == "" {
		http.Error(w, "Не передан обязательный параметр company_id", http.StatusBadRequest)
		return
	}

	var companyID int
	if _, err := fmt.Sscanf(companyIDStr, "%d", &companyID); err != nil || companyID <= 0 {
		http.Error(w, "Некорректный ID заведения", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), companyID) {
		http.Error(w, "Доступ к данному заведению запрещен", http.StatusForbidden)
		return
	}

	supplierUUID := strings.TrimSpace(r.FormValue("supplier_uuid"))

	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		files = r.MultipartForm.File["pdf"]
	}
	if len(files) == 0 {
		files = r.MultipartForm.File["act"]
	}
	if len(files) == 0 {
		http.Error(w, "Файлы акта сверки не найдены", http.StatusBadRequest)
		return
	}

	var textBytes []byte
	var imagesBase64 []string

	ctxCmd, cancelCmd := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelCmd()

	for _, header := range files {
		err := func() error {
			file, err := header.Open()
			if err != nil {
				return err
			}
			defer file.Close()

			cleanFileName := filepath.Base(header.Filename)
			cleanFileName = strings.Map(func(r rune) rune {
				if strings.ContainsRune("[]*?\"\\/", r) {
					return -1
				}
				return r
			}, cleanFileName)
			if cleanFileName == "" {
				cleanFileName = "act.pdf"
			}
			// Create isolated temporary directory for this upload
			tmpDir, err := os.MkdirTemp("", "pdf_rec_*")
			if err != nil {
				return fmt.Errorf("ошибка создания временной директории: %w", err)
			}
			defer os.RemoveAll(tmpDir)

			filePath := filepath.Join(tmpDir, cleanFileName)

			out, err := os.Create(filePath)
			if err != nil {
				return err
			}

			if _, err = io.Copy(out, file); err != nil {
				out.Close()
				return err
			}
			_ = out.Close()

			ext := strings.ToLower(filepath.Ext(cleanFileName))
			allowedExts := map[string]bool{
				".pdf": true, ".png": true, ".jpg": true, ".jpeg": true,
				".webp": true, ".txt": true, ".csv": true, ".xlsx": true, ".xls": true,
			}
			if !allowedExts[ext] {
				return fmt.Errorf("недопустимый тип файла акта сверки %s", ext)
			}

			if ext == ".pdf" {
				txtPath := filepath.Join(tmpDir, "extracted.txt")
				cmdTxt := exec.CommandContext(ctxCmd, "pdftotext", "-layout", filePath, txtPath)
				_ = cmdTxt.Run()
				tb, _ := os.ReadFile(txtPath)
				textBytes = append(textBytes, tb...)
				textBytes = append(textBytes, []byte("\n\n")...)

				imgPrefix := filepath.Join(tmpDir, "img")
				cmdImg := exec.CommandContext(ctxCmd, "pdftoppm", "-jpeg", "-f", "1", "-l", "10", filePath, imgPrefix)
				if err := cmdImg.Run(); err != nil {
					log.Printf("pdftoppm error: %v", err)
				}

				matches, _ := filepath.Glob(imgPrefix + "-*.jpg")
				sort.Slice(matches, func(i, j int) bool {
					getPageNum := func(path string) int {
						base := filepath.Base(path)
						numPart := strings.TrimSuffix(base, ".jpg")
						idx := strings.LastIndex(numPart, "-")
						if idx != -1 {
							if n, err := strconv.Atoi(numPart[idx+1:]); err == nil {
								return n
							}
						}
						return 0
					}
					return getPageNum(matches[i]) < getPageNum(matches[j])
				})
				for _, m := range matches {
					imgBytes, err := os.ReadFile(m)
					if err == nil {
						imagesBase64 = append(imagesBase64, base64.StdEncoding.EncodeToString(imgBytes))
					}
				}
			} else if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
				imgBytes, err := os.ReadFile(filePath)
				if err == nil {
					imagesBase64 = append(imagesBase64, base64.StdEncoding.EncodeToString(imgBytes))
				}
			} else {
				tb, _ := os.ReadFile(filePath)
				textBytes = append(textBytes, tb...)
				textBytes = append(textBytes, []byte("\n\n")...)
			}
			return nil
		}()
		if err != nil {
			log.Printf("Error processing reconciliation file %s: %v", header.Filename, err)
		}
	}

	if len(textBytes) == 0 && len(imagesBase64) == 0 {
		http.Error(w, "Не удалось извлечь текст или изображения из переданных файлов", http.StatusBadRequest)
		return
	}

	aiData, err := parseWithClaude(string(textBytes), imagesBase64, reconciliationPrompt)
	if err != nil {
		http.Error(w, "Сбой распознавания AI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	type ExtractedActItem struct {
		DocNumber string
		DocDate   string
		Amount    float64
	}

	var actItems []ExtractedActItem
	for _, it := range aiData.Items {
		docNum := strings.TrimSpace(it.DocNumber)
		if docNum == "" {
			docNum = strings.TrimSpace(it.Name)
		}
		amt := it.Amount
		if amt == 0 && it.Sum > 0 {
			amt = it.Sum
		}
		if docNum != "" || amt > 0 {
			actItems = append(actItems, ExtractedActItem{
				DocNumber: docNum,
				DocDate:   strings.TrimSpace(it.DocDate),
				Amount:    amt,
			})
		}
	}

	if len(actItems) == 0 {
		http.Error(w, "В акте сверки не найдено операций отгрузки товаров (дебет)", http.StatusBadRequest)
		return
	}

	// Вычисляем временные границы акта сверки (+/- 60 дней)
	var minActDate, maxActDate time.Time
	for _, it := range actItems {
		if it.DocDate != "" {
			t, err := time.Parse("2006-01-02", it.DocDate)
			if err != nil {
				t, err = time.Parse("02.01.2006", it.DocDate)
			}
			if err == nil {
				if minActDate.IsZero() || t.Before(minActDate) {
					minActDate = t
				}
				if maxActDate.IsZero() || t.After(maxActDate) {
					maxActDate = t
				}
			}
		}
	}

	startDate := time.Now().AddDate(-1, 0, 0)
	endDate := time.Now().AddDate(0, 3, 0)
	if !minActDate.IsZero() {
		startDate = minActDate.AddDate(0, 0, -60)
	}
	if !maxActDate.IsZero() {
		endDate = maxActDate.AddDate(0, 0, 60)
	}

	type DbInvoice struct {
		InvoiceNumber string
		DocDate       string
		TotalSum      float64
	}

	dbInvoices := make(map[string]DbInvoice)

	query := `
		SELECT invoice_number, 
		       TO_CHAR(MIN(invoice_date), 'YYYY-MM-DD') as doc_date, 
		       COALESCE(SUM(total_sum), 0) as total_amount
		FROM purchase_history
		WHERE company_id = $1
		  AND ($2 = '' OR supplier_uuid = $2)
		  AND invoice_date >= $3
		  AND invoice_date <= $4
		GROUP BY invoice_number`

	rows, err := db.Query(query, companyID, supplierUUID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var inv DbInvoice
			if err := rows.Scan(&inv.InvoiceNumber, &inv.DocDate, &inv.TotalSum); err == nil {
				key := strings.TrimSpace(inv.InvoiceNumber)
				dbInvoices[key] = inv
			}
		}
	} else {
		log.Printf("⚠️ Ошибка запроса purchase_history для акта сверки: %v", err)
	}

	matched := make([]ReconciliationMatchItem, 0)
	mismatched := make([]ReconciliationMatchItem, 0)
	missingInDb := make([]ReconciliationMatchItem, 0)
	phantomInDb := make([]ReconciliationMatchItem, 0)

	matchedDbInvoices := make(map[string]bool)

	for _, actItem := range actItems {
		docNum := actItem.DocNumber
		dbInv, found := dbInvoices[docNum]
		matchedKey := docNum

		if !found {
			// Проверяем без учета регистра
			for k, v := range dbInvoices {
				if strings.EqualFold(k, docNum) {
					dbInv = v
					found = true
					matchedKey = k
					break
				}
			}
		}

		if !found {
			missingInDb = append(missingInDb, ReconciliationMatchItem{
				DocNumber: actItem.DocNumber,
				DocDate:   actItem.DocDate,
				ActAmount: actItem.Amount,
				DbAmount:  0,
				Diff:      actItem.Amount,
				Status:    "missing_in_db",
			})
		} else {
			matchedDbInvoices[matchedKey] = true
			diff := math.Abs(actItem.Amount - dbInv.TotalSum)
			dateToUse := actItem.DocDate
			if dateToUse == "" {
				dateToUse = dbInv.DocDate
			}

			if diff <= 2.00 {
				matched = append(matched, ReconciliationMatchItem{
					DocNumber: actItem.DocNumber,
					DocDate:   dateToUse,
					ActAmount: actItem.Amount,
					DbAmount:  dbInv.TotalSum,
					Diff:      math.Round(diff*100) / 100,
					Status:    "matched",
				})
			} else {
				mismatched = append(mismatched, ReconciliationMatchItem{
					DocNumber: actItem.DocNumber,
					DocDate:   dateToUse,
					ActAmount: actItem.Amount,
					DbAmount:  dbInv.TotalSum,
					Diff:      math.Round((actItem.Amount-dbInv.TotalSum)*100) / 100,
					Status:    "mismatched",
				})
			}
		}
	}

	// Фантомы: накладные в нашей базе, которых нет в акте сверки поставщика
	for k, dbInv := range dbInvoices {
		if matchedDbInvoices[k] {
			continue
		}
		// Проверяем попадание в диапазон дат акта сверки
		if !minActDate.IsZero() && !maxActDate.IsZero() {
			t, err := time.Parse("2006-01-02", dbInv.DocDate)
			if err == nil {
				if t.Before(minActDate) || t.After(maxActDate) {
					continue
				}
			}
		}

		phantomInDb = append(phantomInDb, ReconciliationMatchItem{
			DocNumber: dbInv.InvoiceNumber,
			DocDate:   dbInv.DocDate,
			ActAmount: 0,
			DbAmount:  dbInv.TotalSum,
			Diff:      math.Round(-dbInv.TotalSum*100) / 100,
			Status:    "phantom_in_db",
		})
	}

	respData := ReconciliationResponseData{
		Matched:     matched,
		Mismatched:  mismatched,
		MissingInDb: missingInDb,
		PhantomInDb: phantomInDb,
		Stats: map[string]interface{}{
			"matched_count":    len(matched),
			"mismatched_count": len(mismatched),
			"missing_count":    len(missingInDb),
			"phantom_count":    len(phantomInDb),
			"total_act_count":  len(actItems),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(respData)
}

func handleGetReconciliationRegistry(w http.ResponseWriter, r *http.Request) {
	companyIDStr := r.URL.Query().Get("company_id")
	companyID, err := strconv.Atoi(companyIDStr)
	if err != nil || companyID <= 0 {
		http.Error(w, "Некорректный company_id", http.StatusBadRequest)
		return
	}

	if !checkAccountantAccessUser(GetAuthUser(r), companyID) {
		http.Error(w, "Доступ запрещен", http.StatusForbidden)
		return
	}

	supplierUUID := strings.TrimSpace(r.URL.Query().Get("supplier_uuid"))
	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")

	if dateFrom == "" || dateTo == "" {
		http.Error(w, "Укажите период (date_from, date_to)", http.StatusBadRequest)
		return
	}

	query := `
		SELECT invoice_number, 
		       TO_CHAR(MIN(invoice_date), 'YYYY-MM-DD') as doc_date, 
		       COALESCE(SUM(total_sum), 0) as total_amount
		FROM purchase_history
		WHERE company_id = $1
		  AND ($2 = '' OR supplier_uuid = $2)
		  AND invoice_date >= $3
		  AND invoice_date <= $4
		GROUP BY invoice_number
		ORDER BY MIN(invoice_date) DESC`

	rows, err := db.Query(query, companyID, supplierUUID, dateFrom, dateTo)
	if err != nil {
		http.Error(w, "Ошибка БД: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var invoices []ReconciliationMatchItem
	for rows.Next() {
		var inv ReconciliationMatchItem
		if err := rows.Scan(&inv.DocNumber, &inv.DocDate, &inv.DbAmount); err == nil {
			invoices = append(invoices, inv)
		}
	}

	if invoices == nil {
		invoices = []ReconciliationMatchItem{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(invoices)
}
