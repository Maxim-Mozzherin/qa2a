package service

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"qa2a/internal/models"
	"qa2a/internal/repository"

	"github.com/jung-kurt/gofpdf"
)

// ReportService отвечает за формирование печатных документов и отчетов в формате PDF.
type ReportService struct {
	repo *repository.Repository
}

// NewReportService создает новый экземпляр сервиса отчетов.
func NewReportService(repo *repository.Repository) *ReportService {
	return &ReportService{repo: repo}
}

// loadFontBytes выполняет каскадный поиск файла шрифта DejaVuSans.ttf для корректной печати кириллицы.
func (s *ReportService) loadFontBytes() ([]byte, error) {
	candidates := []string{
		os.Getenv("FONT_PATH"),
		"fonts/DejaVuSans.ttf",
		"./fonts/DejaVuSans.ttf",
		"/opt/qa2a-reboot/fonts/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	}

	for _, path := range candidates {
		if path == "" {
			continue
		}
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data, nil
		}
	}

	return nil, fmt.Errorf("шрифт DejaVuSans.ttf не найден ни по одному из стандартных путей")
}

// GenerateProcurementPDF генерирует печатный бланк заявки на закупку в формате A4.
func (s *ReportService) GenerateProcurementPDF(reqID int, companyID int, w io.Writer) error {
	// Ищем заявку сначала среди согласованных, затем среди ожидающих и архива
	var target *models.ProcurementRequest

	for _, status := range []string{"approved", "pending", "rejected"} {
		requests, err := s.repo.GetProcurementRequests(companyID, status)
		if err == nil {
			for _, r := range requests {
				if r.ID == reqID {
					target = &r
					break
				}
			}
		}
		if target != nil {
			break
		}
	}

	if target == nil {
		return fmt.Errorf("заявка на закупку #%d не найдена в заведении #%d", reqID, companyID)
	}

	fontBytes, err := s.loadFontBytes()
	if err != nil {
		return fmt.Errorf("ошибка загрузки шрифта UTF-8: %w", err)
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)

	// Регистрируем шрифт с поддержкой кириллицы
	pdf.AddUTF8FontFromBytes("DejaVu", "", fontBytes)
	pdf.AddUTF8FontFromBytes("DejaVu-Bold", "B", fontBytes)

	pdf.AddPage()

	// ==========================================
	// 1. ШАПКА ДОКУМЕНТА
	// ==========================================
	pdf.SetFont("DejaVu", "B", 16)
	pdf.SetTextColor(30, 41, 59) // Slate-800
	pdf.CellFormat(0, 8, fmt.Sprintf("ЗАЯВКА НА ЗАКУПКУ #%d", target.ID), "", 1, "L", false, 0, "")

	pdf.SetFont("DejaVu", "", 10)
	pdf.SetTextColor(100, 116, 139) // Slate-500
	createdAtFormatted := target.CreatedAt.Format("02.01.2006 в 15:04")
	pdf.CellFormat(0, 6, fmt.Sprintf("Дата формирования: %s | Автор: %s", createdAtFormatted, target.UserName), "", 1, "L", false, 0, "")

	statusRus := "Ожидает согласования"
	switch target.Status {
	case "approved":
		statusRus = "Согласовано руководством"
	case "rejected":
		statusRus = "Отклонено"
	}
	pdf.CellFormat(0, 6, fmt.Sprintf("Статус документа: %s", statusRus), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	// Разделительная линия
	pdf.SetDrawColor(226, 232, 240) // Slate-200
	pdf.SetLineWidth(0.5)
	pdf.Line(15, pdf.GetY(), 195, pdf.GetY())
	pdf.Ln(6)

	// ==========================================
	// 2. ЗАГОЛОВОК ТАБЛИЦЫ
	// ==========================================
	drawTableHeader := func() {
		pdf.SetFont("DejaVu", "B", 10)
		pdf.SetFillColor(241, 245, 249) // Slate-100
		pdf.SetTextColor(15, 23, 42)    // Slate-900
		pdf.SetDrawColor(203, 213, 225) // Slate-300
		pdf.SetLineWidth(0.2)

		pdf.CellFormat(10, 8, "№", "1", 0, "C", true, 0, "")
		pdf.CellFormat(85, 8, "Наименование позиции", "1", 0, "L", true, 0, "")
		pdf.CellFormat(25, 8, "Кол-во", "1", 0, "R", true, 0, "")
		pdf.CellFormat(20, 8, "Ед. изм.", "1", 0, "C", true, 0, "")
		pdf.CellFormat(40, 8, "Поставщик", "1", 1, "L", true, 0, "")
	}

	drawTableHeader()

	// ==========================================
	// 3. СТРОКИ СПИСКА ТОВАРОВ
	// ==========================================
	pdf.SetFont("DejaVu", "", 9)
	pdf.SetTextColor(15, 23, 42)

	for idx, item := range target.Items {
		// Проверяем, не требуется ли перенос страницы
		if pdf.GetY() > 270 {
			pdf.AddPage()
			drawTableHeader()
			pdf.SetFont("DejaVu", "", 9)
			pdf.SetTextColor(15, 23, 42)
		}

		// Выделение нечетных строк легким фоном
		fill := idx%2 == 1
		if fill {
			pdf.SetFillColor(248, 250, 252)
		} else {
			pdf.SetFillColor(255, 255, 255)
		}

		supplierDisplay := "—"
		if item.Supplier != "" {
			parts := strings.Split(item.Supplier, "|")
			if len(parts) == 2 {
				supplierDisplay = parts[1]
			} else {
				supplierDisplay = item.Supplier
			}
		}

		posName := item.PositionName
		if item.IsUnlisted {
			posName = "⚠️ " + posName
		}

		// Ограничиваем длину названия в ячейке
		runes := []rune(posName)
		if len(runes) > 42 {
			posName = string(runes[:39]) + "..."
		}

		pdf.CellFormat(10, 7, fmt.Sprintf("%d", idx+1), "1", 0, "C", fill, 0, "")
		pdf.CellFormat(85, 7, posName, "1", 0, "L", fill, 0, "")
		pdf.CellFormat(25, 7, fmt.Sprintf("%.3f", item.Quantity), "1", 0, "R", fill, 0, "")
		pdf.CellFormat(20, 7, item.Unit, "1", 0, "C", fill, 0, "")
		pdf.CellFormat(40, 7, supplierDisplay, "1", 1, "L", fill, 0, "")
	}

	// ==========================================
	// 4. ПОДВАЛ ДОКУМЕНТА
	// ==========================================
	pdf.Ln(8)
	pdf.SetFont("DejaVu", "", 8)
	pdf.SetTextColor(148, 163, 184) // Slate-400
	genTime := time.Now().Format("02.01.2006 15:04:05")
	pdf.CellFormat(0, 5, fmt.Sprintf("Документ сформирован в системе QA2A: %s", genTime), "", 1, "R", false, 0, "")

	if err := pdf.Error(); err != nil {
		return fmt.Errorf("ошибка структуры PDF: %w", err)
	}

	return pdf.Output(w)
}

