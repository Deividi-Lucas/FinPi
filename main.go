package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"finpi/internal/database"
	"finpi/internal/handlers"
)

// Embutir templates HTML diretamente no binário único
//
//go:embed templates/*
var templateFS embed.FS

func main() {
	// Flags de configuração (com suporte a variáveis de ambiente)
	defaultPort := getEnv("PORT", "8080")
	defaultDBPath := getEnv("DB_PATH", "finpi.db")
	defaultHost := getEnv("HOST", "0.0.0.0")

	port := flag.String("port", defaultPort, "Porta HTTP para o servidor")
	dbPath := flag.String("db", defaultDBPath, "Caminho do arquivo do banco SQLite")
	host := flag.String("host", defaultHost, "Host/Interface de rede para bind")
	flag.Parse()

	log.Println("[INFO] Iniciando 🪙 FinPi em modo produção embarcada...")

	// 1. Inicializar Banco SQLite otimizado para Raspberry Pi
	db, err := database.InitDB(*dbPath)
	if err != nil {
		log.Fatalf("[ERRO] Falha ao inicializar banco de dados SQLite: %v", err)
	}
	defer func() {
		log.Println("[INFO] Fechando conexões do banco de dados SQLite...")
		if err := db.Close(); err != nil {
			log.Printf("[AVISO] Erro ao fechar banco: %v", err)
		}
	}()

	// 2. Inicializar Handlers com os templates embutidos
	h, err := handlers.NewHandler(db, templateFS)
	if err != nil {
		log.Fatalf("[ERRO] Falha ao inicializar templates embutidos: %v", err)
	}

	// 3. Configurar Roteador HTTP (Zero dependências externas, compatível com qualquer Go 1.18+)
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.Dashboard)
	mux.HandleFunc("/transactions", h.CreateTransaction)
	mux.HandleFunc("/transactions/update", h.UpdateTransaction)
	mux.HandleFunc("/transactions/delete", h.DeleteTransaction)

	addr := fmt.Sprintf("%s:%s", *host, *port)
	server := &http.Server{
		Addr:           addr,
		Handler:        mux,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   10 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	// 4. Graceful Shutdown (Tratamento de SIGINT / SIGTERM)
	// Essencial no Raspberry Pi para garantir que o SQLite WAL feche com integridade.
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("[INFO] Servidor HTTP ativo em http://%s (armazenamento: %s)", addr, *dbPath)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[ERRO] Servidor HTTP interrompido inesperadamente: %v", err)
		}
	}()

	<-stopChan
	log.Println("[INFO] Sinal de interrupção recebido. Encerrando servidor com segurança...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("[ERRO] Falha durante o shutdown gracioso: %v", err)
	}

	log.Println("[INFO] FinPi finalizado com sucesso.")
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
