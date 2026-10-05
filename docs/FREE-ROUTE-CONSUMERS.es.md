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

## Perfiles, cuota reportada y salud observada

Los nombres `free`, `free-best`, `fast-free`, `reasoning-free`, `coding-best-free` y `long-context-free` siguen siendo rutas virtuales de fallo cerrado. `long-context` es el mismo perfil que `long-context-free`. `local` solo admite llama.cpp cuando el catálogo declara `"local":true`. Ninguno cae a un modelo de pago. Un combo o alias creado por el operador sigue ganando, y ese combo sí puede incluir modelos de pago porque es explícito.

Antes de enviar, el proxy vuelve a mirar la cuota reportada y la salud de esa cuenta. Si una cuenta está agotada o en cooldown y hay otra cuenta elegible, se usa la otra. Si no queda ninguna, la petición falla con `free_route_unavailable`.

La cuota reportada y el cooldown observado no se mezclan. Un 429, un `retryAfter` RFC3339 del cuerpo o un strike de Antigravity alargan el cooldown; no inventan un saldo ni un reset. Un reset ya vencido deja la observación como stale: no acredita que el saldo volvió. Unknown no significa ilimitado. El estado vive en memoria y se pierde al reiniciar.

Antigravity publica en ese registro solo el porcentaje que devolvió su API de cuota. El strike local sigue bloqueando la cuenta en el caché existente, pero no reescribe ese porcentaje como cero.

OpenRouter se descubre en el catálogo público oficial, sin credenciales. Una variante entra al pool gratuito solo si el id termina en `:free` y todas las tarifas explícitas son cero. Consultar el catálogo no autoriza inferencia: hace falta una conexión OpenRouter. `POST /api/admin/health-check` prueba un `provider/model` gratuito concreto, con timeout de 15 s, y no devuelve el texto del modelo ni secretos. No admite perfiles virtuales ni modelos de pago.
