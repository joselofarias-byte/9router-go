# Medición de utilidad para programar (modelos gratis)

Este arnés mide si un modelo gratuito completa tres tareas de programación comparables. No convierte un catálogo, un fixture ni una campaña de marketing en un ranking.

El patrón de salida (JSONL + informe Markdown, pass/fail, latencia, HTTP) sigue el de `scripts/prompt-profile-bench.sh` en la rama de perfiles de prompt (`b2d024e5a49f5f14ac71236611165facb82fa6c8`). Esa rama no se modifica. Cline se clasifica con `discovery.ClineFreeAdapter`. Este `main` no incluye `discovery.OpenRouterAdapter`, así que el arnés aplica en local las mismas reglas de catálogo público: salida de texto, sufijo `:free` y precio cero → `free_tier`. Esas lecturas no envían `Authorization`.

## Tres niveles

| Tier | Qué hace | Red | Credenciales |
| --- | --- | --- | --- |
| `offline_fixture` | Califica soluciones de referencia y clasifica fixtures de catálogo con los mismos adaptadores de discovery | No | No |
| `public_discovery` | GET del catálogo público de OpenRouter y del set `free` de Cline | Sí, solo catálogo | Prohibidas. Si el request llevara `Authorization`, se aborta |
| `authenticated_inference` | Una completion por tarea contra un id exacto de la allowlist | Sí, opt-in | `FREE_CODING_INFERENCE=1` y una clave gratuita dedicada |

`go test` no ejecuta discovery ni inferencia en vivo. Esos caminos están detrás de `FREE_CODING_DISCOVERY=1` (prueba opt-in) o del comando explícito. La inferencia autenticada nunca es requisito de CI.

## Tres tareas

Las tres se califican en local, sin un segundo modelo juez.

| Id | Tarea | Cómo se verifica |
| --- | --- | --- |
| `bugfix` | Corregir `Sum`, que resta en lugar de sumar | `go test` del paquete de la tarea. Los tests son del arnés, no del modelo |
| `refactor` | Hacer que `Average` llame a `Total` y quitar el bucle duplicado | `go test` y un chequeo AST de que `Average` llama a `Total` |
| `review` | Encontrar el bug plantado en `ClampPositive` (`n > 0` está al revés; debería ser `n < 0`) | El texto debe nombrar `ClampPositive`, citar `n > 0` y decir `inverted` o mostrar `n < 0` |

La tarea `review` es un marcador verificable, no una nota de calidad de una revisión libre. El fixture plantado tiene un test que debe fallar; si ese test pasara, el bug no sería real (`TestPlantedBugFails`).

El código devuelto por un modelo se compila y se ejecuta en la máquina local. No acepta imports, `go:embed`, `go:generate` ni cgo. Aun así, hay que lanzar la inferencia autenticada solo en un equipo de confianza.

## Qué se registra en cada corrida

Cada fila JSONL lleva `record_type`:

- `task_run`: provider, model id exacto, `utc`, `quota_scope`, `latency_ms`, `status_429_count`, `compile_ok` / `test_ok` o `review_match`, `reported_cost` solo si el proveedor lo envió, `http_status`, `provenance`
- `catalog_observation`: lectura de catálogo (HTTP, latencia, conteo). No es una tarea de código
- `trial_candidate`: elegible para un ensayo autenticado posterior. Orden alfabético, no de calidad
- `inference_skip`: la inferencia no se ejecutó, con `skip_reason`

`quota_scope` es `none` (fixture), `not_applicable` (catálogo o skip) o `unknown` si la inferencia no trajo cabeceras `X-Ratelimit-*`. No se inventan cupos. Un coste ausente se deja vacío; no se escribe `0` salvo que el proveedor lo haya reportado.

`http_status` `n/a` en el informe significa que no hubo HTTP (el JSON guarda `0`).

## Cómo ejecutarlo

Desde la raíz del repo, con `go` en `PATH`:

```bash
# Offline. No llama a la red.
bash scripts/free-coding-measure.sh

# Discovery público. Sin clave. No es un ranking.
bash scripts/free-coding-measure.sh --tier discovery

# Inferencia. Sin el flag solo escribe SKIP.
bash scripts/free-coding-measure.sh --tier inference --allow .free-coding-out/STAMP/runs.jsonl openrouter/algun-modelo:free
```

La inferencia real:

```bash
FREE_CODING_INFERENCE=1 OPENROUTER_FREE_API_KEY=... \
  bash scripts/free-coding-measure.sh --tier inference \
  --allow .free-coding-out/STAMP/runs.jsonl \
  openrouter/algun-modelo:free
```

El primer segmento es el provider (`openrouter` o `cline`). El resto es el model id y puede contener barras. El `-allow` tiene que ser un JSONL de `offline` o `discover`: solo se aceptan ids que esos adaptadores hayan clasificado como `free` o `free_tier`. En OpenRouter, además, el id debe terminar en `:free`.

Nombres virtuales (`free`, `free-best`, `coding-best-free`, `reasoning-free`, `fast-free`, `long-context`, `local`) se rechazan. No son un model id exacto y podrían enrutar a otro sitio.

El endpoint por defecto es el del proveedor:

- OpenRouter: `https://openrouter.ai/api/v1/chat/completions`
- Cline: `https://api.cline.bot/api/v1/chat/completions`

No se llama a OpenAI ni a otros hosts. No hay reintentos: un 429 cuenta como un intento (`status_429_count=1`) y no se repite. La clave no se imprime ni se guarda en el JSONL.

Salida: `$OUT/runs.jsonl` y `$OUT/report.md`. Por defecto `OUT` es `.free-coding-out/<UTC>/`, ignorado por git.

Equivalente directo:

```bash
go run ./cmd/free-coding-measure offline -out .free-coding-out/manual
go run ./cmd/free-coding-measure discover -out .free-coding-out/manual
go run ./cmd/free-coding-measure report -jsonl .free-coding-out/manual/runs.jsonl
```

## Qué no afirmar

- No decir que un modelo es mejor para programar porque aparece en el catálogo, porque tiene sufijo `:free`, o porque `coding-best-free` lo dejaría pasar. Ese perfil solo exige precio gratis y `tools` declarado.
- No ordenar candidatos de discovery por “calidad”. El informe los lista en orden alfabético. La sección Ranking solo aparece si hay al menos dos modelos con las tres tareas completas, en tier `authenticated_inference`, sin skips. Si no, el informe dice `Ranking withheld`.
- No tratar el self-check `fixture/reference-pass` como un proveedor.
- No rellenar coste ni cuota. `capabilitiesVerified` del health-check del router sigue siendo otra cosa: no certifica estas tareas.
- Una corrida autenticada, cuando exista, solo habla de estas tres tareas, con temperatura 0 y un solo intento. No es un benchmark general.

## Candidatos para un ensayo autenticado posterior

No hubo inferencia autenticada en la construcción de este arnés: el entorno no tenía claves gratuitas (`OPENROUTER_FREE_API_KEY`, `CLINE_FREE_API_KEY`, `FREE_CODING_API_KEY`). La lista de abajo es elegibilidad de catálogo, no una medición de código y no un ranking.

### Fixtures (siempre, sin red)

Salen de los mismos ejemplos que los tests de discovery del arnés de origen, clasificados otra vez: OpenRouter con esas reglas locales y Cline con `ClineFreeAdapter`:

| Provider | Model id | Precio | Tools | ¿Pasa el filtro `coding-best-free`? | Procedencia |
| --- | --- | --- | --- | --- | --- |
| `cline` | `cline-free/muse-spark-1.3-contributor` | `free` | no declarado | no | `cline_test.go` (`free[]`) |
| `cline` | `deepseek/deepseek-v4-flash` | `free` | no declarado | no | `cline_test.go` (`free[]`) |
| `openrouter` | `qwen/coder:free` | `free_tier` | declarado | sí | `openrouter_test.go` (precio cero, sufijo `:free`, tools) |

`qwen/coder:free` es el doble de prueba del adaptador, no una afirmación de que ese id siga vivo en el catálogo. Los dos ids de Cline son el set `free` del fixture; el adaptador ignora `recommended` y `clinePass` (`openai/gpt-6-astra`, `cline-pass/deepseek-v4-pro` no entran). `vendor/mislabeled:free` y `vendor/paid-model` están en el fixture para comprobar que un precio distinto de cero no entra en la allowlist.

Merecen un ensayo autenticado posterior, sin orden de calidad:

1. Ids con `coding_profile_eligible` (precio `free`/`free_tier` y `tools: true`). Coinciden con el filtro `coding-best-free`. En el fixture el único es `openrouter/qwen/coder:free`, y solo como ejemplar.
2. Ids del set `free` de Cline. Ese catálogo no declara tools, así que `coding-best-free` no los selecciona. Sirven para medir las tres tareas, no para llamarlos el mejor modelo de código.

### Snapshot público 2026-10-01T21:04:23Z

Lectura real, sin clave y sin inferencia: OpenRouter devolvió 464 filas de catálogo y 17 `free_tier`; Cline devolvió 4 ids en `free`. No es un ranking. El orden es alfabético. `qwen/coder:free` del fixture no apareció en este snapshot.

Pasan el filtro `coding-best-free` (tools declarado y precio cero). Candidatos al ensayo autenticado de las tres tareas:

| Provider | Model id |
| --- | --- |
| openrouter | `apodex/apodex-1.1-mini:free` |
| openrouter | `cohere/north-mini-code:free` |
| openrouter | `dots-studio/dots-3-note-preview:free` |
| openrouter | `google/gemma-4-26b-a4b-it:free` |
| openrouter | `google/gemma-4-31b-it:free` |
| openrouter | `inclusionai/ling-3.0-flash-sante:free` |
| openrouter | `liquid/lfm-2.5-2.6b:free` |
| openrouter | `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free` |
| openrouter | `nvidia/nemotron-3-super-120b-a12b:free` |
| openrouter | `nvidia/nemotron-3-ultra-550b-a55b:free` |
| openrouter | `nvidia/nemotron-3.5-lightning:free` |
| openrouter | `poolside/laguna-s-2.1:free` |
| openrouter | `poolside/laguna-xs-2.1:free` |
| openrouter | `qwen/qwen3.8-27b:free` |
| openrouter | `thinkingmachines/inkling-small:free` |
| openrouter | `thinkingmachines/inkling:free` |

Set `free` de Cline en el mismo instante. Tools no declarados; no son selección de `coding-best-free`, pero sí candidatos a un ensayo autenticado aparte:

| Provider | Model id |
| --- | --- |
| cline | `cline-free/deepseek-v4.1-flash` |
| cline | `cline-free/mimo-v2.6-flash` |
| cline | `cline-free/muse-spark-1.3-contributor` |
| cline | `stealth/space-bunny-alpha` |

`openrouter/nvidia/nemotron-3.5-content-safety:free` estaba en `free_tier` con `tools: false`. No entra en el filtro de código. No se midió calidad de ninguno de estos ids.

Refrescar la lista, sin clave:

```bash
bash scripts/free-coding-measure.sh --tier discovery
```

El informe dirá `trial candidates=N coding_profile_eligible=M (not a ranking)`. Usar ese `runs.jsonl` como `-allow` el día que haya una clave gratuita. Hasta entonces el arnés queda listo y CI solo corre el tier offline. El snapshot de arriba caduca en cuanto el catálogo cambie.
