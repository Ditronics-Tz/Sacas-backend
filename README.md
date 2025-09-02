# Go Boilerplate

A production-ready Golang web backend project scaffold using the Gin web framework with JWT authentication, comprehensive RBAC, OTP integration, and extensive security features.

## Features

- **Clean Architecture**: Well-organized project structure with handlers, services, repositories, and models
- **Authentication & Authorization**: JWT-based auth with user registration, login, and role-based access control (RBAC)
- **Three-Tier Role System**: User, Administrator, and Super Admin roles with hierarchical permissions
- **Account Security**: 
  - Email verification with OTP
  - Password reset with OTP
  - Secure cookie handling
  - Password hashing with bcrypt
- **Comprehensive Security**:
  - CSRF protection
  - Input validation and sanitization
  - XSS protection
  - SQL injection prevention
  - Path traversal protection
  - Security headers
- **OTP Integration**: Send OTP codes for 2FA, email verification, and password resets via email (SendGrid) or SMS (Twilio)
- **Database**: PostgreSQL with GORM ORM, including migrations and seeding
- **Caching**: Redis integration for OTP storage and session management
- **API Health Tracking**: Middleware to track API metrics, requests per endpoint, and success/failure counts
- **Comprehensive Logging**: Structured logging with configurable levels
- **User Management**: Full CRUD operations for user management with proper authorization
- **Error Handling**: Comprehensive error handling with proper HTTP status codes

## Project Structure

```
├── cmd/                    # Main applications for this project
│   └── api/               # Main API server
├── internal/              # Private application code
│   ├── config/           # Configuration utilities
│   ├── controllers/      # HTTP handlers (auth, user, OTP)
│   ├── database/         # Database initialization, migrations, and seeding
│   ├── middlewares/      # HTTP middleware (auth, CSRF, validation, security)
│   ├── models/           # Database models with validation
│   ├── repositories/     # Data access layer
│   ├── routes/           # API route definitions
│   ├── services/         # Business logic services (notifications)
│   └── utils/            # Utility functions
├── pkg/                  # Shared packages
│   └── logger/           # Comprehensive logging utilities
└── .env                  # Environment configuration
```

## API Endpoints

### Authentication
- **POST /api/auth/register** - Register a new user with email verification
- **POST /api/auth/login** - Login and get JWT token (supports both header and cookie auth)
- **POST /api/auth/verify-email** - Verify email address with OTP
- **POST /api/auth/forgot-password** - Initiate password reset
- **POST /api/auth/reset-password** - Reset password with OTP
- **POST /api/auth/resend-verification** - Resend email verification OTP
- **POST /api/auth/logout** - Logout and clear authentication cookie

### OTP Management
- **POST /api/otp/send** - Send OTP to email or phone
- **POST /api/otp/verify** - Verify OTP

### User Management (Protected)
- **GET /api/protected/profile** - Get current user profile
- **PUT /api/protected/change-password** - Change user password
- **GET /api/protected/users** - List all users (Admin+)
- **GET /api/protected/users/:id** - Get user by ID (Admin+)
- **POST /api/protected/users** - Create new user (Admin+)
- **PUT /api/protected/users/:id** - Update user (Admin+)
- **DELETE /api/protected/users/:id** - Delete user (Admin+)

### Admin Dashboard (Administrator & Super Admin)
- **GET /api/protected/admin/dashboard** - Admin dashboard
- **GET /api/protected/admin/users/stats** - User statistics

### Super Admin Dashboard (Super Admin only)
- **GET /api/protected/superadmin/dashboard** - Super admin dashboard
- **GET /api/protected/superadmin/system/info** - System information

### System
- **GET /api/health** - Check API health with system info
- **GET /api/metrics** - View API usage metrics

## Role-Based Access Control

### User Roles
1. **User** (`user`): Standard user with basic access
2. **Administrator** (`administrator`): Can manage users and access admin features
3. **Super Admin** (`super_admin`): Full system access including system configuration

### Permission Hierarchy
- **Super Admin**: Can perform all actions including managing other super admins
- **Administrator**: Can manage users and access admin features, but cannot manage super admins
- **User**: Can only access their own profile and basic features

## Setup Instructions

### Prerequisites
- Go 1.20+
- PostgreSQL
- Redis
- SendGrid account (for email)
- Twilio account (for SMS)

### Local Development

1. Clone the repository and navigate to the project directory.

2. Copy `.env.example` to `.env` and update the environment variables:
   ```bash
   cp .env.example .env
   ```

3. Install dependencies:
   ```bash
   go mod tidy
   ```

4. Set up PostgreSQL database:
   ```sql
   CREATE DATABASE go_boilerplate;
   ```

5. Run the application:
   ```bash
   go run ./cmd/api
   ```
   The server will start on the port specified in `.env` (default: 8080).

6. The application will automatically:
   - Run database migrations
   - Create a default super admin user (email: admin@example.com, password: password)

### Using Docker
```bash
# Build the application
docker build -t go-boilerplate .

# Run with docker-compose (includes PostgreSQL and Redis)
docker-compose up -d
```

## Environment Variables

Create a `.env` file based on `.env.example` with the following variables:

```env
# Server Configuration
PORT=8080
ENV=development
LOG_LEVEL=info

# Database
DATABASE_URL=host=localhost user=postgres password=postgres dbname=go_boilerplate port=5432 sslmode=disable

# Redis
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=

# JWT
JWT_SECRET=your-super-secret-jwt-key

# Email (SendGrid)
SENDGRID_API_KEY=your_sendgrid_api_key
FROM_EMAIL=noreply@example.com

# SMS (Twilio)
TWILIO_ACCOUNT_SID=your_twilio_account_sid
TWILIO_AUTH_TOKEN=your_twilio_auth_token
FROM_PHONE=your_twilio_phone_number

# Security Features
CSRF_ENABLED=true
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS_PER_MINUTE=60
```

## Security Features

### Input Validation
- Comprehensive input sanitization
- Protection against SQL injection
- XSS prevention
- Path traversal protection
- Content-Type validation
- Request size limits

### Authentication Security
- JWT tokens with expiration
- Secure HTTP-only cookies
- Password hashing with bcrypt
- Account activation via email
- Password reset with OTP verification

### CSRF Protection
- Token-based CSRF protection
- Secure token generation and validation
- Redis-backed token storage

### Security Headers
- X-Content-Type-Options: nosniff
- X-Frame-Options: DENY
- X-XSS-Protection: 1; mode=block
- Content-Security-Policy
- Referrer-Policy: strict-origin-when-cross-origin

## Database Schema

The application uses PostgreSQL with the following main entities:

### Users Table
```sql
- id (Primary Key)
- created_at, updated_at, deleted_at
- email (Unique)
- password (Hashed with bcrypt)
- first_name, last_name
- phone_number (Unique, Optional)
- role (user/administrator/super_admin)
- is_active (Boolean)
- is_verified (Boolean)
- last_login_at
```

## Testing

Run tests with:
```bash
go test ./...
```

## Production Considerations

1. **Environment Variables**: Ensure all sensitive variables are properly set
2. **Database**: Use connection pooling and proper indexing
3. **Redis**: Configure persistence and memory limits
4. **SSL/TLS**: Use HTTPS in production
5. **Rate Limiting**: Configure appropriate rate limits
6. **Monitoring**: Set up proper logging and monitoring
7. **Backup**: Implement database backup strategy

## Default Credentials

After initial setup, a super admin user is created:
- **Email**: admin@example.com
- **Password**: password
- **Role**: super_admin

**Important**: Change these credentials immediately in production!

## Contributing

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request

## License

MIT License