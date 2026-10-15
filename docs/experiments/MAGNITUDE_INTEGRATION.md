# Magnitude: evaluacion de integracion con Capimux

Estado: experimental; no desplegado. Rama: `experiment/magnitude-provider`.

## Hipotesis
Magnitude puede conectarse como endpoint OpenAI-compatible sin modificar la arquitectura principal de Capimux. **No esta verificado** que el proveedor existente admita todas las capacidades necesarias.

Fuentes: https://github.com/magnitudedev/magnitude y https://docs.magnitude.dev

## Validacion previa (sin instalar)
1. Identificar en el codigo del router el registro de proveedores, la configuracion de base URL y la traduccion OpenAI/Anthropic.
2. Confirmar endpoints reales, autenticacion, modelo y streaming del servidor Magnitude.
3. Comprobar compatibilidad de tool calls, SSE, cancelacion, errores, contexto y conteo de tokens.
4. Inventariar CPU, arquitectura, RAM, GPU, almacenamiento y limites de Oracle A1/A2.
5. Estimar memoria de modelos antes de descargar o iniciar procesos.

## Prueba controlada (solo tras autorizacion de ejecucion)
- Escuchar exclusivamente en 127.0.0.1 o proteger el servicio con autenticacion y red privada.
- Registrar Magnitude como proveedor opt-in, sin afectar rutas existentes.
- Probar GET /v1/models y POST /v1/chat/completions en modo normal y streaming, si estan soportados.
- Repetir con modelo, cuantizacion, longitud de contexto y prompts equivalentes frente a Ollama.
- Medir TTFT, tokens/s prefill y decode, RAM/VRAM, errores y latencia p50/p95.
- Evitar credenciales en logs y artefactos.
- No usar Oracle con creditos promocionales sin control de costo y autorizacion.

## Criterios para avanzar
- Compatibilidad funcional confirmada y pruebas de regresion sin fallos.
- Mejora reproducible sobre la alternativa en hardware propio.
- Ruta de rollback documentada; servicio opcional y deshabilitado por defecto.
- Si la API existente basta, preferir configuracion a codigo nuevo.

## Estado de verificacion
- [x] Rama aislada creada.
- [x] README de ambos proyectos consultado.
- [ ] Adaptador existente identificado en codigo.
- [ ] Contrato HTTP contrastado contra servidor real.
- [ ] Benchmarks realizados.
- [ ] PR abierto.
