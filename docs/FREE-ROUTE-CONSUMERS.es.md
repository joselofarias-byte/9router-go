# Ruta gratis para otros proyectos

`free` y `free-best` son modelos virtuales de este proxy. TBM, NewTermux, Nightzuku, OpenCode y cualquier otro cliente compatible con OpenAI pueden usarlos como recurso gratuito sin crear un combo.

## Cómo apuntar el cliente

El cliente habla con este proxy, no con el proveedor de pago.

- URL base: `http://127.0.0.1:20128/v1` (o el `PORT` en el que esté escuchando 9router-go)
- Modelo: `free-best` o `free`
- Los dos nombres usan el mismo pool. La puntuación pone primero al mejor candidato y el resto queda como reserva.

Ejemplo:

```bash
curl http://127.0.0.1:20128/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"free-best","messages":[{"role":"user","content":"hola"}]}'
```

`GET /v1/models` también anuncia `free` y `free-best`.

Si el proxy local exige clave, envíala como `Authorization: Bearer ...`. No pongas en el cliente las claves de Groq, DeepSeek, Cline ni de ningún proveedor: esas quedan en las conexiones locales de 9router-go.

## Qué entra en el pool

En cada petición el proxy arma una cadena de reserva con modelos que el registro de Fabric marca ahora mismo como `free` o `free_tier`, y solo si ese proveedor tiene una conexión local activa. Quedan fuera los modelos de pago, los no clasificados, los inactivos y los desconectados.

## Fail-closed: no hay caída silenciosa a un modelo de pago

Si no hay ningún modelo elegible, la petición falla. El nombre `free` o `free-best` no se reenvía a OpenAI, Anthropic ni DeepSeek.

Lo mismo ocurre a mitad de la cadena. Antes de cada salto del pool virtual, el proxy vuelve a comprobar precio gratuito, proveedor activo y cuenta conectada. Si un candidato pasó a ser de pago, se desactivó o se desconectó, se omite. Si no queda ninguno, la petición falla con el mismo error. No se sustituye por un proveedor de pago solo porque el modelo pedido se llamaba `free` o `free-best`.

Un alias o un combo creado por el usuario con el nombre `free` o `free-best` sigue ganando. Ese combo es explícito: si incluye modelos de pago, se respetan. La revalidación gratuita solo aplica al pool virtual.

## Error `free_route_unavailable`

Cuando el pool está vacío o todos los saltos dejaron de ser elegibles, la respuesta es HTTP 503 y un JSON al estilo OpenAI:

```json
{
  "error": {
    "message": "free route unavailable: no eligible free or free-tier models with an active local connection",
    "type": "server_error",
    "code": "free_route_unavailable"
  }
}
```

El cliente debe tratar `code == "free_route_unavailable"` como “ahora no hay recurso gratuito”, no como un fallo del modelo de pago ni como una invitación a reintentar contra otro proveedor por su cuenta. El cuerpo no incluye claves ni tokens.

En Go, el mismo caso se reconoce con `errors.Is(err, chat.ErrFreeRouteUnavailable)`.

## Perfiles gratuitos incorporados

Desde esta revisión, `/v1/models` anuncia los siguientes perfiles. Cada salto
revalida conexión, precio, disponibilidad y capacidades declaradas:

| Perfil | Selección |
| --- | --- |
| `free`, `free-best` | Pool gratuito general, ordenado por puntuación |
| `coding-best-free` | Modelos gratuitos cuyo catálogo declara soporte de tools |
| `reasoning-free` | Modelos gratuitos cuyo catálogo declara reasoning |
| `fast-free` | Menor latencia observada; modelos sin mediciones al final |
| `long-context` | Contexto declarado de al menos 128000 tokens |
| `local` | Solo llama.cpp local; conserva la restricción de loopback |

`coding-best-free` es un filtro de capacidades, no un resultado de benchmark.
Las capacidades declaradas no equivalen a herramientas/contexto probados. El
router todavía no calcula tokens del pedido para asegurar que todo el contexto
quepa; `long-context` solo exige el mínimo declarado. `paid-fallback` debe ser un
combo explícito creado por el operador; ningún perfil gratuito cae a pago.
Los aliases/combos explícitos conservan precedencia, incluso con estos nombres.

## OpenRouter y salud observada

El discovery consulta el catálogo oficial público de OpenRouter. Conserva IDs
completos como `openrouter/qwen/model:free` y solo clasifica `free_tier` cuando
existe la variante `:free` y todas las tarifas explícitas son cero. Tarifas
faltantes, inválidas, adicionales o schedules no vacíos excluyen el modelo del
pool gratuito. Hace falta registrar una conexión OpenRouter legítima: consultar
el catálogo no concede acceso de inferencia. Discovery inicia a los 30 segundos
y actualiza cada 24 horas; un catálogo leído vacío desactiva modelos de esa
fuente, mientras que un error de red conserva la clasificación anterior.

El tráfico de perfiles virtuales alimenta un estado en memoria aislado por
proveedor/modelo/cuenta: éxitos, EWMA de latencia/TTFT y cooldown. Un 429/402
bloquea temporalmente el candidato durante un minuto; 5xx/red durante 2 segundos;
401/403/404 pasan además por las cuarentenas existentes de confianza. Son
intervalos locales de reintento, no saldos ni resets de cuotas del proveedor.
Errores de pedido 4xx y cancelaciones no envenenan la salud. El estado se pierde
al reiniciar. Las conexiones múltiples existentes no multiplican cuotas
compartidas por proyecto/organización/proveedor.

## Diagnóstico y prueba activa

Estas rutas usan la protección API-key configurada para el router. No exponer
el router con autenticación deshabilitada fuera del dispositivo.

- `GET /api/admin/registry`: snapshot sin secretos de autenticación.
- `GET /api/admin/explain-route?model=coding-best-free`: candidatos actuales,
  usando el mismo estado de salud y política que el routing.
- `POST /api/admin/health-check`: una prueba de texto opt-in de un modelo
  concreto elegible, con timeout 15 s y `max_tokens=16`; consume cuota.

```bash
curl http://127.0.0.1:20128/api/admin/health-check \
  -H 'Content-Type: application/json' \
  -d '{"model":"llamacpp/qwen-local"}'
```

La respuesta incluye `healthy`, `latencyMs` y `capabilitiesVerified:false`.
No devuelve texto del modelo, credenciales ni errores upstream. No admite
prompts arbitrarios, modelos pagos o perfiles virtuales. Una respuesta 200 sin
un mensaje de texto válido se considera fallo temporal. Esta prueba no certifica
calidad de programación, tools, contexto máximo ni consumo facturado real.

Para verificar solo el catálogo oficial sin claves ni inferencia:

```bash
OPENROUTER_CATALOG_LIVE=1 go test ./internal/controlplane/discovery \
  -run TestOpenRouterOfficialCatalogLive -v
```
