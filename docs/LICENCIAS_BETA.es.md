# Licencias beta — diseño y estado

Fecha: 2026-10-04

## Estado real

El sistema de licencia/entitlement todavía no está implementado en `main`.

La separación Community/Pro y el plan de capacidades ya están definidos. Si la prioridad es repartir betas externas, la licencia beta debe pasar a ser el siguiente bloque de implementación.

## Por qué necesitamos una licencia beta antes de repartir binarios Pro

Si se entrega una compilación con todas las funciones Pro activas y sin gate, esa copia puede seguir funcionando con esas funciones aunque el producto cambie después.

Para probar con grupos externos hay que separar:

- Community, que debe seguir siendo útil;
- capacidades experimentales Pro, habilitadas durante un período controlado.

## Lo mínimo necesario

No hace falta tener pagos, dLocal, Stripe ni un servidor comercial completo.

La primera versión puede usar una licencia firmada:

```text
license_id
channel = beta
issued_at
expires_at
features
optional tester id
signature
```

Ejemplo conceptual:

```json
{
  "license_id": "beta-00127",
  "channel": "beta",
  "expires_at": "2026-11-30T23:59:59Z",
  "features": [
    "fabric.advanced_routing",
    "fabric.multi_account_policy",
    "telemetry.advanced"
  ]
}
```

La aplicación verifica la firma con una clave pública embebida. La clave privada que emite licencias no se distribuye.

Esto permite entregar betas que vencen, renovar sin recompilar, habilitar funciones por tester y probar Free vs Pro sin depender todavía de un servidor permanente.

## Regla de confianza

Una licencia vencida no debe borrar configuración, bloquear datos, romper Community ni impedir exportar.

Sólo deshabilita las capacidades Pro correspondientes.

## Arquitectura

```text
License/Entitlement Provider
          |
          v
Feature capabilities
          |
          +-- fabric.advanced_routing
          +-- fabric.multi_account_policy
          +-- fabric.intent_profiles
          +-- telemetry.advanced
          +-- policy.budget
```

El routing no debe conocer Stripe, dLocal ni ningún procesador de pago.

Más adelante puede cambiarse:

```text
beta signed file
      ->
license server
      ->
billing backend
```

sin reescribir Fabric.

## Orden de implementación

### B0 — interfaz de entitlement
Crear una interfaz que responda si una capability está habilitada. Community es el fallback seguro cuando no hay licencia.

### B1 — licencia firmada offline
Formato versionado, expiración, capabilities, firma asimétrica, validación local y tests de manipulación/expiración.

### B2 — UI beta
Mostrar Beta activa, vencimiento, capacidades habilitadas, importación de licencia y estado Community si no hay licencia.

### B3 — herramienta interna de emisión
Herramienta separada del binario público para emitir/renovar licencias sin exponer la clave privada.

### B4 — backend comercial
Sólo después de validar interés real: login, compra, renovación, seats y procesador de pagos.

## Cuándo podemos buscar testers

Se puede empezar a reclutar testers ya.

No conviene distribuir una beta Pro externa hasta que B0, B1 y una UI B2 mínima pasen tests y el build esté claramente identificado como beta.

No se fija una fecha inventada. El gate es funcional: cuando esos puntos estén verificados, la beta puede salir.
