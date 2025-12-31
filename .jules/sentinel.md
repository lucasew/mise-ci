# 🛡️ Sentinel's Journal - Critical Learnings

This journal is for CRITICAL security learnings only. No routine work.
Format: ## YYYY-MM-DD - [Title] **Vulnerability:** [What you found] **Learning:** [Why it existed] **Prevention:** [How to avoid next time]

## 2024-07-17 - Timing Attack via Logical Short-Circuiting
**Vulnerability:** A timing side-channel existed in the `checkBasicAuth` function in `internal/server/auth.go`. The use of a short-circuiting logical OR (`||`) operator meant that the function's response time differed depending on whether the `Authorization` header was missing versus when it was present but contained invalid credentials.
**Learning:** This vulnerability existed because while `subtle.ConstantTimeCompare` was correctly used for comparing secrets, the overall logical flow of the checks was not constant-time. A logical operator that short-circuits can undermine the security of constant-time components by creating a measurable timing difference in the control flow itself.
**Prevention:** When combining multiple security-sensitive checks (like header presence, token validity, and secret comparison), all checks must be performed. Use non-short-circuiting operations, such as bitwise operators (`&`), to combine the results of each check. This ensures the execution path remains the same length, regardless of which individual check fails.
