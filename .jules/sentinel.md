## 2024-08-01 - Robust Credential Sanitization
**Vulnerability:** The `sanitizeArgs` function in `internal/core/service.go` used a highly specific, case-sensitive check for the username `x-access-token` to redact credentials from URLs. This approach was brittle and could easily fail to prevent the leakage of credentials in logs if different usernames or casing were used.
**Learning:** Relying on specific string checks for credential sanitization is a flawed security practice. A more robust, generalized approach is required to effectively prevent sensitive data exposure. The solution is to parse arguments as URLs and redact any password found in the user information, regardless of the username.
**Prevention:** Implement generalized sanitization routines that do not depend on specific credential formats. Always assume that credentials can appear in unexpected formats and design sanitization logic to be as comprehensive as possible.

## 2024-08-02 - Command Sanitization
**Vulnerability:** The `runCommandSync` and `runCommandCapture` functions in `internal/core/service.go` sanitized the command's arguments but not the command itself before logging. This could lead to the exposure of sensitive information in the logs if a command were to be constructed with secrets in it.
**Learning:** It's important to sanitize all parts of a command, not just the arguments, before logging. A secret can be passed as part of the command itself, and this can be overlooked when focusing only on the arguments. The fix was to wrap the command in `sanitizeArgs` before logging.
**Prevention:** Always sanitize all parts of a command before logging, including the command name and any subcommands.
