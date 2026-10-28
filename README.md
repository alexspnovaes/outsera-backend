# Outsera Back-end

API REST desenvolvida para o desafio do Golden Raspberry Awards. A aplicação importa o arquivo de filmes para um SQLite em memória e informa os produtores com o menor e o maior intervalo entre vitórias consecutivas.

O banco é criado a cada execução e não depende de nenhum serviço externo. O driver `modernc.org/sqlite` é implementado em Go e não exige CGO para executar a aplicação.

## Requisitos

- Go 1.27.3
- Git

## Como executar

```sh
git clone https://github.com/alexspnovaes/outsera-backend.git
cd outsera-backend
go run ./cmd/api
```

Por padrão, a API utiliza `data/Movielist.csv` e fica disponível em `http://127.0.0.1:8080`.

```sh
curl http://127.0.0.1:8080/api/movies/maxMinWinIntervalForProducers
```

As configurações podem ser alteradas por variáveis de ambiente:

- `HOST`: endereço IP de escuta. Padrão: `127.0.0.1`.
- `PORT`: porta HTTP entre 1 e 65535. Padrão: `8080`.
- `CSV_PATH`: caminho do arquivo CSV. Padrão: `data/Movielist.csv`.

## Endpoint

`GET /api/movies/maxMinWinIntervalForProducers`

Resposta para o arquivo fornecido no desafio:

```json
{
  "min": [
    {
      "producer": "Joel Silver",
      "interval": 1,
      "previousWin": 1990,
      "followingWin": 1991
    }
  ],
  "max": [
    {
      "producer": "Matthew Vaughn",
      "interval": 13,
      "previousWin": 2002,
      "followingWin": 2015
    }
  ]
}
```

A mesma rota aceita `HEAD`. Outros métodos retornam `405` com o cabeçalho `Allow`, caminhos desconhecidos retornam `404` e falhas internas retornam `500` sem expor detalhes da aplicação. As respostas com corpo são JSON.

O endpoint segue o nível 2 do modelo de maturidade de Richardson, conforme solicitado no desafio.

## Arquivo de entrada

O CSV usa `;` como separador e deve conter estas colunas, nesta ordem:

```text
year;title;studios;producers;winner
```

O leitor aceita BOM UTF-8 e campos entre aspas. O ano deve estar entre 1 e 9999; título, estúdio e produtor são obrigatórios; `winner` aceita `yes`, `no` ou vazio. Estúdios são separados por vírgula, enquanto produtores são separados por vírgula e por ` and `.

O arquivo é validado por completo antes da gravação e arquivos acima de 8 MB são rejeitados. A troca do catálogo ocorre em uma transação: se a leitura ou a persistência falhar, os dados anteriores são mantidos.

## Cálculo dos intervalos

As vitórias são agrupadas por produtor e ordenadas por ano. Para cada produtor, o cálculo considera somente prêmios consecutivos.

Vitórias em 2000, 2002 e 2008, por exemplo, produzem intervalos de 2 e 6 anos. Todos os empates nos intervalos mínimo e máximo são retornados. Se nenhum produtor tiver vencido duas vezes, `min` e `max` são arrays vazios.

## Organização do projeto

```text
cmd/api                  configuração e inicialização da aplicação
internal/domain          entidades e regra de cálculo dos intervalos
internal/application     casos de uso e interfaces de persistência
internal/adapter/csvfile leitura do arquivo CSV
internal/adapter/sqlite  persistência em SQLite
internal/adapter/httpapi endpoint e representação JSON
```

O domínio não depende de HTTP, CSV ou SQLite. O pacote `application` coordena os casos de uso por meio de interfaces pequenas, e `cmd/api` conecta as implementações concretas.

## Testes

O desafio solicita somente testes de integração. A suíte utiliza SQLite real em memória e `httptest`, exercitando o caminho completo entre CSV, aplicação, banco de dados e endpoint HTTP.

```sh
go test -count=1 ./...
go vet ./...
go build ./cmd/api
```

Os testes validam o resultado oficial do CSV, empates, intervalos no mesmo ano, dados inválidos, rollback da importação, relacionamentos no banco e respostas HTTP.

Na inicialização, a importação termina antes de o servidor aceitar requisições. Ao receber `SIGINT` ou `SIGTERM`, a aplicação encerra o servidor de forma controlada, aguardando por até 10 segundos as requisições em andamento.
