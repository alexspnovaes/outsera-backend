# Outsera Back-end

API REST em Go para consultar os indicados e vencedores da categoria Pior Filme do Golden Raspberry Awards. A aplicação importa o CSV na inicialização e identifica os produtores com o menor e o maior intervalo entre vitórias consecutivas. Também oferece as consultas utilizadas pelo dashboard e pela listagem de filmes do [front-end](https://github.com/alexspnovaes/outsera-frontend).

O servidor utiliza `net/http` e SQLite em memória. O driver `modernc.org/sqlite` é implementado em Go, dispensando CGO e a instalação de um serviço de banco de dados.

## Requisitos e execução

- Go 1.23 ou superior.
- Git para clonar o repositório.

```sh
git clone https://github.com/alexspnovaes/outsera-backend.git
cd outsera-backend
go mod download
go run ./cmd/api
```

Execute os comandos na raiz do projeto para que o caminho padrão do CSV seja resolvido corretamente. A API fica disponível em `http://localhost:8080`.

O arquivo é validado e importado antes de o servidor aceitar requisições. Se houver erro no arquivo ou na configuração, a inicialização é interrompida e o motivo é registrado no terminal. Cada execução cria um banco novo a partir do CSV.

Para verificar a disponibilidade:

```sh
curl http://localhost:8080/health
```

Resposta esperada:

```json
{"status":"ok"}
```

## Configuração

As configurações são lidas de variáveis de ambiente:

- `PORT`: porta HTTP, com valor padrão `8080`. Aceita um inteiro entre `1` e `65535`.
- `CSV_PATH`: caminho do arquivo de entrada, com valor padrão `data/Movielist.csv`. Caminhos relativos são resolvidos a partir do diretório de execução.
- `CORS_ORIGINS`: lista de origens permitidas, separadas por vírgula. O padrão é `http://localhost:5173,http://localhost:4173,http://127.0.0.1:5173,http://127.0.0.1:4173`, contemplando o servidor de desenvolvimento e a prévia de produção do front-end.

Exemplo PowerShell:

```powershell
$env:PORT = "8081"
$env:CSV_PATH = "data/Movielist.csv"
$env:CORS_ORIGINS = "http://localhost:5173"
go run ./cmd/api
```

## Endpoints

As rotas de consulta utilizam `GET` e retornam JSON.

### Filmes

`GET /api/movies`

Lista filmes em ordem crescente de ID. Os identificadores seguem a posição de cada registro importado do CSV. Parâmetros opcionais:

- `page`: número da página, começando em `0`; padrão `0`.
- `size`: quantidade por página, entre `1` e `100`; padrão `15`.
- `year`: ano entre `1` e `9999`.
- `winner`: `true` para vencedores ou `false` para os demais indicados.

```sh
curl "http://localhost:8080/api/movies?year=1990&winner=true&page=0&size=15"
```

A resposta contém os filmes em `content` e os metadados de paginação, incluindo `totalElements`, `totalPages`, `number`, `size`, `first` e `last`. Uma consulta sem correspondências retorna `content: []`.

`GET /api/movies/{id}`

Consulta um filme pelo identificador. Retorna `404` quando o ID válido não corresponde a um filme.

### Anos com múltiplos vencedores

`GET /api/movies/yearsWithMultipleWinners`

Retorna um objeto com a propriedade `years`. Cada item contém `year` e `winnerCount`, considerando apenas os anos com mais de um filme vencedor.

### Vitórias por estúdio

`GET /api/movies/studiosWithWinCount`

Retorna um objeto com a propriedade `studios`. Cada item contém `name` e `winCount`. O resultado inclui os estúdios com ao menos uma vitória, em ordem decrescente de quantidade, com desempate pelo nome em ordem crescente.

### Intervalos entre vitórias de produtores

`GET /api/movies/maxMinWinIntervalForProducers`

Retorna todos os pares de vitórias consecutivas que atingem o menor e o maior intervalo. Com o arquivo `data/Movielist.csv`, o resultado é:

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

### Vencedores de um ano

`GET /api/movies/winnersByYear?year=2018`

Retorna um array de filmes vencedores no ano informado. O parâmetro `year` é obrigatório e aceita valores entre `1` e `9999`. Sem vencedores para o ano, a resposta é `[]`.

### Saúde da aplicação

`GET /health`

Verifica a conexão com o banco e retorna `{"status":"ok"}` quando a consulta é bem-sucedida.

### Validação e erros

Parâmetros inválidos, desconhecidos ou repetidos nas rotas de filmes retornam `400`. Métodos não suportados retornam `405`, com o cabeçalho `Allow`. Falhas internas retornam `500`; os detalhes são registrados no servidor. O corpo de erro segue o formato:

```json
{"error":"mensagem"}
```

As rotas também atendem `OPTIONS` para preflight de CORS. Origens autorizadas recebem o cabeçalho `Access-Control-Allow-Origin`; um preflight de origem não permitida retorna `403`.

## Importação e regras de cálculo

O arquivo de entrada usa ponto e vírgula como separador e deve conter o cabeçalho `year;title;studios;producers;winner`. A leitura aceita BOM UTF-8. Cada registro deve ter cinco campos, ano válido, título e ao menos um estúdio e um produtor. O campo `winner` aceita `yes`, `no` ou vazio; vazio representa um filme que não venceu.

Estúdios são separados por vírgula. Produtores são separados por vírgula e pela sequência ` and `. A importação remove espaços nas extremidades e nomes repetidos no mesmo filme. A palavra `and` presente em nomes de estúdios é preservada. Os nomes são tratados como identificadores exatos, mantendo a grafia fornecida no arquivo.

O CSV inteiro é validado antes de alterar o banco. A importação substitui o conjunto de dados dentro de uma transação. Filmes, estúdios e produtores possuem tabelas próprias, com tabelas de associação e chaves estrangeiras.

O cálculo ordena as vitórias por produtor, ano e ID do filme e compara pares consecutivos de um mesmo produtor. Por exemplo, para vitórias em 2000, 2002 e 2008, os intervalos são `2` e `6` anos. Todos os empates no mínimo e no máximo são preservados, com ordem determinística de produtor e anos. Dois filmes vencedores do mesmo produtor no mesmo ano representam dois prêmios e geram um intervalo de `0`. Caso nenhum produtor possua duas vitórias, `min` e `max` são arrays vazios.

## Organização do projeto

```text
cmd/api/main.go                     configuração e ciclo de vida do servidor
internal/movie/model.go             contratos dos dados e paginação
internal/movie/csv.go               leitura e validação do CSV
internal/movie/store.go             schema, importação e consultas SQLite
internal/movie/service.go           regra dos intervalos consecutivos
internal/httpapi/handler.go          rotas, validação HTTP, JSON e CORS
internal/httpapi/integration_test.go testes de integração CSV + SQLite + HTTP
data/Movielist.csv                  arquivo de entrada
```

O pacote `movie` concentra os dados, a persistência e a regra dos intervalos. O pacote `httpapi` traduz requisições HTTP em consultas e respostas JSON. O ponto de entrada configura essas dependências e controla a inicialização e o encerramento do servidor.

## Verificação

Na raiz do projeto:

```sh
go test -race -count=1 ./...
go vet ./...
go build ./cmd/api
```

A suíte utiliza testes de integração com SQLite real em memória e um servidor HTTP `httptest`, sem mocks. Cobre o arquivo fornecido, filtros, paginação, consultas do dashboard, intervalos consecutivos, empates, intervalo zero, registros desordenados, deduplicação de nomes, resultados vazios, importação inválida sem alteração dos dados anteriores, validação HTTP, métodos e CORS.

O CSV incluído contém 206 filmes e 42 vencedores. Os resultados de referência dos intervalos são Joel Silver, com 1 ano entre 1990 e 1991, e Matthew Vaughn, com 13 anos entre 2002 e 2015.

## Integração e operação

O [Outsera Front-end](https://github.com/alexspnovaes/outsera-frontend) utiliza esta API e deve ser executado em um processo separado. Durante o desenvolvimento, o proxy do Vite encaminha `/api` para `http://localhost:8080`. Para acessar o back-end diretamente em outro endereço, configure `VITE_API_BASE_URL` com a URL completa da coleção, como `http://localhost:8081/api/movies`, e inclua a origem da interface em `CORS_ORIGINS`.

Os dados são somente de leitura pela API e permanecem em memória durante a execução. Para atualizar o conjunto de filmes, substitua o CSV e reinicie o processo. O servidor aplica limites de tempo às conexões e permite até dez segundos para concluir requisições durante o encerramento por sinal do sistema.
