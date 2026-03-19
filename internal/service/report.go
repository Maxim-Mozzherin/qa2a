package service

import (
	"fmt"
	"io"
	"os"
	"github.com/jung-kurt/gofpdf"
	"qa2a/internal/models"
	"qa2a/internal/repository"
)

type ReportService struct { repo *repository.Repository }
func NewReportService(repo *repository.Repository) *ReportService { return &ReportService{repo: repo} }

func (s *ReportService) GenerateProcurementPDF(reqID int, companyID int, w io.Writer) error {
	requests, _ := s.repo.GetProcurementRequests(companyID, "approved")
	var target *models.ProcurementRequest
	for _, r := range requests {
		if r.ID == reqID { target = &r; break }
	}
	if target == nil { return fmt.Errorf("заявка не найдена") }

	pdf := gofpdf.New("P", "mm", "A4", "")
	
	// Загружаем шрифт
	bReg, err := os.ReadFile("/opt/qa2a-reboot/fonts/DejaVuSans.ttf")
	if err != nil { return fmt.Errorf("шрифт не найден: %v", err) }
	pdf.AddUTF8FontFromBytes("DejaVu", "", bReg)

	pdf.AddPage()
	pdf.SetFont("DejaVu", "", 16)
	pdf.Cell(0, 10, "ЗАЯВКА #"+fmt.Sprint(target.ID))
	pdf.Ln(12)

	// Рисуем таблицу без заливки (Fill), чтобы не было ошибок
	pdf.SetFont("DejaVu", "", 12)
	pdf.Cell(90, 10, "Наименование")
	pdf.Cell(30, 10, "Кол-во")
	pdf.Cell(40, 10, "Ед. изм.")
	pdf.Ln(10)

	// Линия разделителя
	pdf.Line(10, 32, 170, 32)
	pdf.Ln(2)

	pdf.SetFont("DejaVu", "", 11)
	for _, item := range target.Items {
		pdf.Cell(90, 8, item.PositionName)
		pdf.Cell(30, 8, fmt.Sprintf("%.2f", item.Quantity))
		pdf.Cell(40, 8, item.Unit)
		pdf.Ln(8)
	}

	return pdf.Output(w)
}