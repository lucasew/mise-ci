## 2024-08-01 - Robust Credential Sanitization
**Vulnerability:** The `sanitizeArgs` function in `internal/core/service.go` used a highly specific, case-sensitive check for the username `x-access-token` to redact credentials from URLs. This approach was brittle and could easily fail to prevent the leakage of credentials in logs if different usernames or casing were used.
**Learning:** Relying on specific string checks for credential sanitization is a flawed security practice. A more robust, generalized approach is required to effectively prevent sensitive data exposure. The solution is to parse arguments as URLs and redact any password found in the user information, regardless of the username.
**Prevention:** Implement generalized sanitization routines that do not depend on specific credential formats. Always assume that credentials can appear in unexpected formats and design sanitization logic to be as comprehensive as possible.

## 2024-08-02 - HTTP Security Headers
**Vulnerability:** The application did not set any HTTP security headers, leaving it vulnerable to common web attacks such as clickjacking and Cross-Site Scripting (XSS).
**Learning:** HTTP security headers are a simple and effective way to harden a web application. By implementing headers such as `X-Frame-Options`, `X-Content-Type-Options`, and `Content-Security-Policy`, we can significantly reduce the attack surface of the application.
**Prevention:** All new web applications should be configured to set appropriate security headers by default. Existing applications should be audited to ensure they are not missing this basic security control.
