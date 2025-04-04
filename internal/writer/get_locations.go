package writer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func (w *Writer) GetLocations(wr http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(wr, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	limit, err := parseQueryParam(r, "limit", 10)
	if err != nil {
		http.Error(wr, err.Error(), http.StatusBadRequest)
		return
	}

	offset, err := parseQueryParam(r, "offset", 0)
	if err != nil {
		http.Error(wr, err.Error(), http.StatusBadRequest)
		return
	}

	locations, err := w.db.GetLocations(r.Context(), limit, offset)
	if err != nil {
		http.Error(wr, fmt.Sprintf("Error fetching locations: %v", err), http.StatusInternalServerError)
		return
	}

	wr.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(wr).Encode(locations); err != nil {
		http.Error(wr, "Error encoding locations to JSON", http.StatusInternalServerError)
	}
}

func parseQueryParam(r *http.Request, paramName string, defaultValue int) (int, error) {
	paramStr := r.URL.Query().Get(paramName)

	if paramStr == "" {
		return defaultValue, nil
	}

	parsedValue, err := strconv.Atoi(paramStr)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value: %v", paramName, err)
	}

	return parsedValue, nil
}
