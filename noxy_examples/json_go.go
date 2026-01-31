package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// Endereço aninhado
type Endereco struct {
	Rua    string `json:"rua"`
	Cidade string `json:"cidade"`
	Estado string `json:"estado"`
	CEP    string `json:"cep,omitempty"`
}

// Contato com diferentes tipos
type Contato struct {
	Tipo  string `json:"tipo"`
	Valor string `json:"valor"`
}

// Configurações com map dinâmico
type Configuracoes struct {
	Notificacoes bool           `json:"notificacoes"`
	Tema         string         `json:"tema"`
	Extras       map[string]any `json:"extras,omitempty"`
}

// Estrutura principal com composição
type Usuario struct {
	ID            int            `json:"id"`
	Nome          string         `json:"nome"`
	Email         string         `json:"email"`
	Ativo         bool           `json:"ativo"`
	Saldo         float64        `json:"saldo"`
	Tags          []string       `json:"tags"`
	Endereco      Endereco       `json:"endereco"`
	Contatos      []Contato      `json:"contatos"`
	Configuracoes Configuracoes  `json:"configuracoes"`
	Metadata      map[string]any `json:"metadata"`
	CriadoEm      time.Time      `json:"criado_em"`
	Senha         string         `json:"-"` // ignorado no JSON
}

func main() {
	// Criando estrutura complexa
	usuario := Usuario{
		ID:    1,
		Nome:  "João Silva",
		Email: "joao@email.com",
		Ativo: true,
		Saldo: 1500.75,
		Tags:  []string{"premium", "developer", "early-adopter"},
		Senha: "senha123", // não aparece no JSON
		Endereco: Endereco{
			Rua:    "Rua das Flores, 123",
			Cidade: "São Paulo",
			Estado: "SP",
			CEP:    "01234-567",
		},
		Contatos: []Contato{
			{Tipo: "telefone", Valor: "+55 11 99999-9999"},
			{Tipo: "linkedin", Valor: "linkedin.com/in/joaosilva"},
		},
		Configuracoes: Configuracoes{
			Notificacoes: true,
			Tema:         "dark",
			Extras: map[string]any{
				"idioma":   "pt-BR",
				"timezone": "America/Sao_Paulo",
				"beta":     true,
			},
		},
		Metadata: map[string]any{
			"origem":      "organic",
			"campanhaId":  42,
			"score":       98.5,
			"permissoes":  []string{"read", "write", "admin"},
			"ultimoLogin": nil,
		},
		CriadoEm: time.Now(),
	}

	// Marshal
	jsonBytes, err := json.MarshalIndent(usuario, "", "  ")
	if err != nil {
		panic(err)
	}

	fmt.Println("=== MARSHAL ===")
	fmt.Println(string(jsonBytes))

	// Unmarshal
	var usuarioRecuperado Usuario
	err = json.Unmarshal(jsonBytes, &usuarioRecuperado)
	if err != nil {
		panic(err)
	}

	fmt.Println("\n=== UNMARSHAL ===")
	fmt.Printf("Nome: %s\n", usuarioRecuperado.Nome)
	fmt.Printf("Saldo: R$ %.2f\n", usuarioRecuperado.Saldo)
	fmt.Printf("Cidade: %s\n", usuarioRecuperado.Endereco.Cidade)
	fmt.Printf("Tags: %v\n", usuarioRecuperado.Tags)
	fmt.Printf("Primeiro contato: %s - %s\n", usuarioRecuperado.Contatos[0].Tipo, usuarioRecuperado.Contatos[0].Valor)
	fmt.Printf("Tema: %s\n", usuarioRecuperado.Configuracoes.Tema)
	fmt.Printf("Idioma: %v\n", usuarioRecuperado.Configuracoes.Extras["idioma"])
	fmt.Printf("Origem: %v\n", usuarioRecuperado.Metadata["origem"])
	fmt.Printf("Senha recuperada: '%s' (vazia pois foi ignorada)\n", usuarioRecuperado.Senha)
}
