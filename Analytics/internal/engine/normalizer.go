package engine

import (
	"analytics_service/internal/taxonomy"
)

// ProductClassification представляет результат классификации для движка аудита
type ProductClassification = taxonomy.ProductClassification

// CleanUnit очищает единицы измерения товара iiko (обертка над пакетом taxonomy)
func CleanUnit(rawUnit, category string) string {
	return taxonomy.CleanUnit(rawUnit, category)
}

// ClassifyProduct выполняет глубокую классификацию номенклатуры iiko (обертка над пакетом taxonomy)
func ClassifyProduct(rawName string) ProductClassification {
	return taxonomy.ClassifyProduct(rawName)
}
