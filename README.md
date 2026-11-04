# SpidiAgent

Agente intermediario para la pasarela de pagos SPIDI. Multi-tenant, concurrente, con SQLite para auditoría y colas con rate limiting.

## Características

- **Multi-tenant**: cada comercio con sus propias credenciales SPIDI
- **SQLite con WAL**: registro completo de operaciones, auditoría, e historial
- **Colas serializadas de escritura**: nunca SQLITE_BUSY
- **Rate limiting + retry exponencial** hacia la API de SPIDI
- **Token management**: cache y refresh automático del JWT
- **Cifrado AES-256-GCM** de contraseñas en reposo (Argon2 key derivation)
- **Graceful shutdown** con drenado de colas
- **Middleware HTTP** con auth por tenant key y logging estructurado
- **Binario puro Go** (sin CGO, compilable con `CGO_ENABLED=0`)

## Estructura

```
spidi-agent/
├── cmd/server/         # entrypoint
├── internal/
│   ├── config/         # carga de config YAML
│   ├── crypto/         # AES-GCM, bcrypt, argon2
│   ├── database/       # SQLite WAL + worker serializado
│   ├── handlers/       # HTTP endpoints (API REST)
│   ├── logger/         # slog wrapper
│   ├── middleware/     # auth + logging
│   ├── models/         # estructuras de datos
│   ├── queue/          # rate limiter + request queue
│   ├── service/        # orquestación SPIDI + BD
│   ├── spidi/          # cliente HTTP contra API SPIDI
│   └── tenant/         # CRUD de tenants
├── config.yaml.example
├── go.mod
└── go.sum
```

## Endpoints

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/health` | Estado del servicio |
| POST | `/clientes` | Registrar tenant (short_name, password, bank_account_id, environment) |
| GET | `/clientes` | Listar tenants |
| POST | `/pagos` | Crear sesión de pago (tenant_id, monto, moneda, orden) |
| GET | `/pagos/:id` | Consultar estado del pago |
| GET | `/pagos` | Listar pagos (historial) |
| POST | `/webhook` | Recibir notificaciones de SPIDI |

## Flujo de uso

```bash
# 1. Compilar (sin CGO)
CGO_ENABLED=0 go build -o spidi-agent ./cmd/server

# 2. Iniciar
export SPIDI_MASTER_KEY="clave-segura-de-32+"
./spidi-agent ./config.yaml

# 3. Crear un pago (cliente no ve SPIDI)
curl -X POST http://localhost:8484/pagos \
  -H "Content-Type: application/json" \
  -H "X-Tenant-Key: api-key-del-cliente" \
  -d '{"tenant_id":"cliente1","monto":50,"moneda":"USD","orden":"ORD-001"}'

# 4. Respuesta
{
  "success": true,
  "data": {
    "session_id": "3ddc4cfb-...",
    "payment_url": "https://sandbox.mispidi.com/?id=3ddc4cfb-...",
    "status": "pending"
  }
}
```

## Base de datos SQLite

### Tabla `operation_log`
Cada operación (crear pago, consultar estado, etc.) se registra con:
- tenant_id, operation, resource_id, status
- request_json, response_json (para trazabilidad)
- duration_ms, client_ip, error_message
- timestamp

### Tabla `audit_chain`
Para triggers manuales de cambios en registros críticos.

### Tabla `sessions`
Sesiones de pago persistidas con estado, monto, moneda, URLs.

## Seguridad

- Contraseñas cifradas con AES-256-GCM en SQLite (master key derivada con Argon2)
- API keys de tenants (se registran via middleware)
- Constant-time comparisons
- Sin CGO = menos superficie de ataque
- WAL mode con busy_timeout para evitar corrupción en concurrencia

## Pendientes

- [ ] Persistir tokens JWT en BD (para restart sin re-login)
- [ ] Implementar triggers de audit_chain automáticos
- [ ] Agregar endpoint de expiración de sesiones
- [ ] Implementar webhook signing verification
- [ ] Tests unitarios y de integración
- [ ] Métricas Prometheus
- [ ] Docker image
- [ ] Swagger/OpenAPI spec

## Licencia

Propio / Privado
