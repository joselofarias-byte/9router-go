# Cómo elige una ruta 9router/Fabric

Fecha: 2026-10-04

Este documento explica el selector actual sin vender humo. Hay partes ya activas y otras preparadas para la siguiente etapa.

## La idea en una frase

9router no le pregunta a una IA "qué modelo te parece mejor". Construye una lista de opciones elegibles, descarta las que no deben usarse, les asigna un puntaje y ordena las candidatas. Después, al momento de ejecutar, elige una cuenta utilizable y aplica fallback si algo falla.

## 1. Elegibilidad

El Control Plane recorre los modelos descubiertos y descarta proveedores desactivados, modelos desactivados, modelos que no cumplen la política pedida, nodos en quarantine/disabled y cuentas inactivas.

Para una ruta free-only, sólo acepta exactamente `free` o `free_tier`. Un precio desconocido no se considera gratis.

Código real simplificado de `internal/controlplane/routing/policy.go`:

```go
for providerID, providerModels := range state.ProviderModels {
    provider := state.Providers[providerID]
    if provider == nil || !provider.IsActive {
        continue
    }

    for _, model := range providerModels {
        if model == nil || !model.IsActive {
            continue
        }

        if requestedModel != "" && model.ModelID != requestedModel {
            continue
        }

        isFree := IsFreePricing(model.PricingMode)
        if policy == PolicyFreeOnly && !isFree {
            continue
        }

        // después se evalúan las cuentas activas de este proveedor
    }
}
```

## 2. Puntaje base

Cada combinación provider/model/account recibe un `Score`.

El motor ya tiene dimensiones para confianza, tasa de éxito, TTFT, coste/free tier, calidad/benchmark y riesgo de la superficie/cuenta.

Versión simplificada de `internal/controlplane/scoring/engine.go`:

```go
total :=
    trustScore +
    successRateScore +
    performanceScore +
    costScore +
    qualityScore -
    accountRiskPenalty
```

Importante: hoy no todas esas dimensiones reciben datos reales en el selector base. `SelectCandidates` ya conecta trust, free/free-tier y penalización de riesgo. Las dimensiones de success rate, TTFT y benchmark están preparadas, pero todavía no se alimentan de manera general para todos los modelos.

## 3. Perfiles virtuales gratuitos

Hoy existen:

- `free`
- `free-best`
- `fast-free`
- `reasoning-free`
- `coding-best-free`
- `long-context-free`

No son modelos reales. Construyen dinámicamente un pool de opciones gratuitas elegibles.

```text
coding-best-free
      |
      +-- sólo free/free_tier
      +-- sólo proveedor/cuenta activos
      +-- score base
      +-- salud reciente
      +-- bonus de coding
      +-- ordenar
      +-- probar el primero
      +-- si falla, probar el siguiente
```

## 4. Salud reciente

Para `free-best`, el router usa hasta 300 requests de las últimas 6 horas.

Por modelo calcula muestras, éxitos, tasa de éxito, latencia media de respuestas exitosas y si el resultado más reciente fue éxito o fallo.

La lógica central, simplificada:

```go
successRate := successes / samples
adjustment := (successRate - 0.5) * 30

if latestSuccess {
    adjustment += 8
} else {
    adjustment -= 12
}

if avgLatency <= 1500 {
    adjustment += 5
} else if avgLatency <= 3500 {
    adjustment += 3
} else if avgLatency <= 7000 {
    adjustment += 1
}

effectiveScore := baseScore + adjustment
```

Si un modelo está en quarantine, recibe una penalización que lo saca de la competencia normal.

## 5. "Mejor para programar"

`coding-best-free` usa la salud reciente y agrega un bonus pequeño por señales de especialización.

Versión simplificada del código real:

```go
for _, marker := range []string{
    "coder", "codex", "codestral",
    "devstral", "starcoder", "-code", "code-",
} {
    if strings.Contains(modelName, marker) {
        bonus = 6
    }
}

caps := GetCapabilitiesForModel(provider, model)
if caps.Reasoning && caps.Tools && bonus < 2 {
    bonus = 2
}
```

El ajuste adaptativo se limita a ±29 puntos. Esto evita que una simple etiqueta "coder" borre una diferencia grande de seguridad o riesgo.

## 6. "Más rápido"

`fast-free` prioriza latencia medida, pero conserva la señal de confiabilidad.

```text
<= 0,75 s   +6
<= 1,5 s    +5
<= 3 s      +4
<= 5 s      +3
<= 8 s      +2
<= 15 s     +1
```

## 7. "Razonamiento" y "contexto largo"

`reasoning-free` filtra por capacidad de razonamiento conocida.

`long-context-free` agrega bonus según la ventana de contexto:

```text
>= 1M       +6
>= 512K     +5
>= 256K     +4
>= 200K     +3
> 128K      +2
```

## 8. Después del modelo viene la cuenta

`internal/handlers/chat/connections.go` selecciona una cuenta concreta. Antes de usarla descarta cuentas desactivadas, excluidas por un intento anterior, en cooldown, con model lock, con cuota Codex/Antigravity agotada o incompatibles con una asignación estricta.

Versión simplificada:

```go
for _, account := range accounts {
    if disabled(account) {
        continue
    }
    if inCooldown(account) {
        continue
    }
    if modelLocked(account, model) {
        continue
    }
    if quotaExhausted(account, model) {
        continue
    }
    return account
}
```

Después aplica la estrategia configurada: prioridad, round-robin/sticky o random.

## 9. Qué falta para el selector Pro

La siguiente evolución es `CapacitySnapshot`: una representación común de capacidad por provider/account/model.

Fabric deberá recibir de forma uniforme:

```text
remaining
used
reset_at
cooldown_until
known / unknown / stale / exhausted
scope de la cuota
origen de la señal
momento de observación
```

Entonces podrá decidir, por ejemplo:

```text
Claude cuenta A: 8% semanal -> reservar
Gemini cuenta B: 72% -> preferir
Codex cuenta C: cooldown -> excluir
modelo local: disponible -> fallback sin coste
```

La regla central:

> Primero descartar lo que no corresponde usar. Después puntuar lo que sí. Y siempre poder explicar por qué ganó una ruta.

## 10. Qué significa realmente "mejor"

No existe un mejor modelo universal.

Fabric busca el mejor para una intención bajo restricciones: mejor para programar, mejor gratuito, más rápido, más confiable ahora, local/privado, mejor sin gastar una cuota escasa o mejor antes de que una cuota se reinicie.
