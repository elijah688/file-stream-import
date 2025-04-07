package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
)

const forwardURL = "http://localhost:9090/process"

func main() {
	http.HandleFunc("/", serveHTML)

	fmt.Println("Server started on :8080")
	http.ListenAndServe(":8080", nil)
}

func serveHTML(w http.ResponseWriter, r *http.Request) {
	wd, err := os.Getwd()
	if err != nil {
		http.Error(w, "Error getting current working directory", http.StatusInternalServerError)
		return
	}

	templatePath := filepath.Join(wd, "templates", "index.html")

	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		http.Error(w, "Template file not found", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		http.Error(w, "Error parsing template", http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, nil)
	if err != nil {
		http.Error(w, "Error executing template", http.StatusInternalServerError)
	}
}
