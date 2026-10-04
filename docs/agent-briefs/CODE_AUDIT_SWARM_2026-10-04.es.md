# Auditoría multi-modelo del código — plan de trabajo

Fecha: 2026-10-04

## Objetivo

Hacer una revisión del repositorio con modelos diferentes y comparar hallazgos, sin asumir que un solo modelo detectará todos los problemas.

GitHub sigue siendo la fuente de verdad. Ningún agente debe modificar `main` directamente.

## Principio

Usar el modelo más fuerte para arquitectura y riesgos sistémicos. Usar modelos especializados o económicos para revisiones repetibles y concretas.

No votar por mayoría: un hallazgo se acepta sólo cuando está respaldado por código, test, reproducción o argumento técnico verificable.

## Pasada A — arquitectura y riesgos sistémicos

Modelo recomendado: GPT-6 Astra en Work/Codex cuando haya cuota disponible.

Buscar:

- inconsistencias entre Control Plane y Data Plane;
- estados duplicados de quota/cooldown/health;
- carreras y snapshots stale;
- rutas que puedan saltarse fail-closed;
- exposición de secretos;
- cambios de upstream que rompan Fabric;
- acoplamientos que dificulten Community/Pro;
- problemas de migración/rollback;
- huecos de tests en decisiones críticas.

Salida: hallazgos priorizados P0/P1/P2 con archivo, símbolo, evidencia y test recomendado.

## Pasada B — revisión específica de Go

Candidatos del router:

- codex/gpt-5.6-sol-review
- codex/codex-auto-review
- codex/gpt-5.3-codex-spark-review
- kimi/kimi-k2.7-code
- kimi/kimi-for-coding
- xai/grok-code-fast-1
- alicode/qwen3-coder-next
- alicode/qwen3-coder-plus

Usar uno o dos, no todos, sobre los mismos cambios críticos para comparar hallazgos.

Buscar:

- errores de concurrencia;
- nil/error handling;
- condiciones de borde;
- fugas de goroutines/conexiones;
- incoherencias de locking;
- errores de parsing;
- paths no cubiertos por tests;
- contratos rotos entre paquetes.

## Pasada C — modelos gratuitos de programación

El repo ya incluye `scripts/free-coding-measure.sh`.

Antes de confiar en un modelo gratuito:

```bash
bash scripts/free-coding-measure.sh --tier discovery
```

Después ejecutar las tres tareas del arnés sólo contra ids que sigan activos y clasificados como gratuitos.

Candidatos observados en octubre de 2026 deben tratarse como temporales: la disponibilidad cambia.

Objetivo: elegir 1–2 modelos gratuitos que sean suficientemente buenos para revisión rutinaria y reservar modelos caros para arquitectura o conflictos.

## Pasada D — auditorías temáticas independientes

Asignar una preocupación por agente:

1. Seguridad / secretos / auth.
2. Routing y fallback.
3. Quota/cooldown/multi-account.
4. Persistencia / SQLite / snapshots / migraciones.
5. Concurrencia.
6. API compatibility OpenAI/Claude/Gemini.
7. Dashboard y coherencia con backend.
8. Tests faltantes y falsos positivos.
9. Licencias/entitlements y degradación Community.
10. Termux/ARM64/multiplataforma.

Un agente no debe arreglar fuera de su alcance durante la auditoría. Primero reporta.

## Formato obligatorio de hallazgo

```text
ID:
Severidad:
Archivo/símbolo:
Qué puede pasar:
Por qué:
Cómo reproducir o demostrar:
Test que falta:
Cambio mínimo sugerido:
Confianza:
```

No aceptar frases genéricas tipo "podría mejorar" sin evidencia.

## Fusión de resultados

El coordinador agrupa duplicados y clasifica:

- confirmado;
- probable, necesita test;
- diseño/opinión;
- falso positivo.

Los hallazgos confirmados se convierten en ramas/PR pequeñas.

## Uso de cuota

Astra no se usa para tareas mecánicas.

Prioridad de Astra:

1. arquitectura completa;
2. revisión final de decisiones de seguridad/licenciamiento;
3. contradicciones entre auditores;
4. PRs de alto riesgo.

Revisión cotidiana: Sol-review/Codex especializado/Kimi/Qwen/modelos gratuitos medidos.

## Regla de privacidad

No enviar código privado o secretos a endpoints gratuitos/stealth sin revisar sus políticas de datos.

El repositorio público puede auditarse con estos modelos, pero tokens, bases de datos, cookies y credenciales siguen fuera de cualquier prompt.
