package application

import "jarvis/backend/internal/modules/transactions/domain"

func isValidIdempotencyKey(key string) bool {
	if key == "" || len(key) > domain.MaxIdentifierBytes {
		return false
	}
	for index := 0; index < len(key); index++ {
		if key[index] < '!' || key[index] > '~' {
			return false
		}
	}
	return true
}
