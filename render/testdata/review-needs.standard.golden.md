## ⚠ Review verdict: needs-attention

_Target: commit abc1234_

Two issues: a race condition and a SQL injection vulnerability.

### Findings (2)

| # | Severity | Location | Title |
|---|---|---|---|
| 1 | 🔴 critical | `auth.go:35-37` | Race condition: mutex removed |
| 2 | 🟠 high | `auth.go:50-52` | SQL injection |

#### 1. Race condition: mutex removed `[critical, confidence 1.00]`

**File**: `auth.go:35-37`

Concurrent map writes without locking will panic in Go.

**Recommendation**: Restore s.mu.Lock() / defer s.mu.Unlock().

#### 2. SQL injection `[high, confidence 0.95]`

**File**: `auth.go:50-52`

User input is concatenated into the query string.

**Recommendation**: Use db.QueryRow with $1 placeholder.

### Next steps

1. Restore the mutex lock
2. Use parameterized SQL

_Tokens used: 4008_
