# Focus Tagging Guide

Systematic approach to tagging Go codebases for focused context extraction. Applicable to manual tagging and LLM-assisted auto-tagging.

## Syntax Reference

### Basic Format
```go
// @focus: #group { tag1, tag2 }
package mypackage
```

### Placement Rules
- Must appear before `package` statement
- Must be a line comment starting with `// @focus:`
- Multiple focus lines are accumulated
- Build tags (`//go:build`, `// +build`) must precede focus tags

### Valid Examples
```go
// @focus: #core { ecs }
package systems

// @focus: #core { types, interfaces }
// @focus: #render { pipeline }
package engine

//go:build linux
// @focus: #platform { linux }
package compat
```

### Special Tags

| Tag | Behavior |
|-----|----------|
| `#all` | File always included in output regardless of selection |

Use `#all` sparingly for truly universal dependencies (core types, constants, interfaces).

## Tagging Philosophy

### Purpose
Tags create a semantic index of the codebase orthogonal to package structure. They enable:
- Cross-cutting concern selection (select all "auth" regardless of package)
- Feature-based context extraction (select entire "payment" flow)
- Dependency-aware subsetting (select "cache" + its dependencies)

### Core Principles

**1. Tag by Concern, Not Location**
```go
// GOOD: semantic concern
// @focus: #auth { session, token }

// BAD: repeating package structure
// @focus: #handlers { auth }
```

**2. Multiple Groups per File**
Files typically participate in multiple concerns:
```go
// @focus: #core { ecs } #game { collision } #physics { aabb }
package collision
```

**3. Granular Tags over Broad Groups**
```go
// GOOD: specific, composable
// @focus: #data { cache, redis }

// BAD: too broad, loses utility
// @focus: #infrastructure { data }
```

**4. Consistent Vocabulary**
Establish and maintain tag vocabulary across codebase. Document in project root.

## Group Design Strategies

### By System/Module
Groups represent major system boundaries:
```
#core      - fundamental types, interfaces, utilities
#auth      - authentication, authorization, sessions
#data      - persistence, caching, migrations
#api       - HTTP handlers, middleware, routing
#worker    - background jobs, queues, scheduling
#notify    - notifications, email, push
```

### By Feature/Domain
Groups represent business domains:
```
#payment   - checkout, billing, subscriptions
#inventory - stock, warehouse, fulfillment
#user      - profiles, preferences, accounts
#search    - indexing, queries, ranking
#analytics - events, metrics, reporting
```

### By Layer (Use Sparingly)
Architectural layers—less useful for cross-cutting selection:
```
#model     - domain entities
#service   - business logic
#handler   - HTTP/gRPC handlers
#repo      - data access
```

### Hybrid Approach (Recommended)
Combine system and domain groups:
```go
// @focus: #core { types } #payment { checkout }
package checkout

// @focus: #data { postgres } #payment { persistence }
package payment_repo

// @focus: #api { handlers } #payment { endpoints }
package payment_api
```

## Tag Naming Conventions

### Groups
- Lowercase, single word preferred
- No special characters
- Noun or noun phrase

| Good | Avoid |
|------|-------|
| `#auth` | `#authentication-module` |
| `#cache` | `#caching_layer` |
| `#payment` | `#PaymentSystem` |

### Tags
- Lowercase, single word or short compound
- Describes specific concept within group
- Verb-noun for actions, noun for entities

| Good | Avoid |
|------|-------|
| `session` | `session-management` |
| `validate` | `validation_logic` |
| `redis` | `redis-cache-impl` |

## Common Patterns

### Entry Points
Tag main packages and command entry points:
```go
// @focus: #cmd { server }
package main

// @focus: #cmd { worker }
package main

// @focus: #cmd { migrate }
package main
```

### Shared Types
Core types used across the codebase:
```go
// @focus: #all
// @focus: #core { types, errors }
package types
```

### Interface Definitions
```go
// @focus: #core { interfaces }
// @focus: #data { contracts }
package interfaces
```

### Implementations
Tag with both interface group and implementation specifics:
```go
// @focus: #data { cache, redis }
package redis_cache

// @focus: #data { cache, memory }
package mem_cache
```

### HTTP Handlers
```go
// @focus: #api { handlers, middleware }
// @focus: #auth { endpoints }
package auth_handlers
```

### Database/Repository
```go
// @focus: #data { postgres, queries }
// @focus: #user { persistence }
package user_repo
```

### Business Logic
```go
// @focus: #service { logic }
// @focus: #payment { processing, validation }
package payment_service
```

### External Integrations
```go
// @focus: #integration { stripe }
// @focus: #payment { gateway }
package stripe_client

// @focus: #integration { sendgrid }
// @focus: #notify { email }
package email_client
```

### Utilities
```go
// @focus: #util { strings, crypto }
package util

// @focus: #util { http, retry }
package httputil
```

### Tests (Optional)
If including test files, tag by what they test:
```go
// @focus: #test { unit }
// @focus: #payment { checkout }
package checkout_test
```

## Anti-Patterns

### Over-Tagging
```go
// BAD: too many tags, loses focus
// @focus: #core { types } #data { model } #payment { entity } #api { dto } #service { domain }
package payment
```

**Fix:** Choose 2-3 most relevant concerns.

### Under-Tagging
```go
// BAD: single vague tag
// @focus: #misc { util }
package payment_validator
```

**Fix:** Tag by actual concern: `#payment { validation }`

### Redundant Tags
```go
// BAD: tag repeats package name
// @focus: #payment { payment }
package payment
```

**Fix:** Use descriptive sub-concerns: `#payment { checkout, refund }`

### Inconsistent Vocabulary
```go
// File A
// @focus: #auth { authentication }

// File B  
// @focus: #security { auth }

// File C
// @focus: #user { login }
```

**Fix:** Standardize: `#auth { login, session, token }`

### Hierarchical Tags in Flat Structure
```go
// BAD: encoding hierarchy in tags
// @focus: #payment-checkout-validation { rules }
```

**Fix:** Use groups for hierarchy: `#payment { checkout, validation }`

## LLM Auto-Tagging Guidelines

### Analysis Process

1. **Package Analysis**
    - Package name and path indicate primary domain
    - Import statements reveal dependencies and integrations
    - Exported types indicate public API surface

2. **File Content Analysis**
    - Struct names suggest domain entities
    - Interface definitions indicate contracts
    - Function names reveal operations
    - Comments may explicitly state purpose

3. **Cross-Reference Analysis**
    - Files importing this file need related tags
    - Files this imports may share tags
    - Test files indicate tested functionality

### Heuristics

| Signal | Suggested Group | Suggested Tags |
|--------|-----------------|----------------|
| `http.Handler`, `gin.Context`, `echo.Context` | `#api` | `handlers`, `middleware` |
| `sql.DB`, `*gorm.DB`, `*sqlx.DB` | `#data` | `postgres`, `mysql`, `queries` |
| `redis.Client`, `*redis.Pool` | `#data` | `cache`, `redis` |
| `jwt`, `oauth`, `session` in names | `#auth` | `token`, `session`, `oauth` |
| `context.Context` heavy usage | `#core` | `context` |
| `error` types, `errors.New` | `#core` | `errors` |
| `_test.go` suffix | `#test` | `unit`, `integration` |
| `main` package | `#cmd` | binary name |
| `/internal/` path | consider `#internal` | package name |
| `/pkg/` path | consider `#pkg` | package name |
| `Interface` suffix types | `#core` | `interfaces` |
| Heavy generics usage | `#core` | `generics` |

### Output Format

Generate tags in order of relevance:
```go
// @focus: #primary_group { most_relevant_tag } #secondary_group { tag1, tag2 }
```

Maximum 3 groups per file. Maximum 4 tags per group.

### Confidence Thresholds

| Confidence | Action |
|------------|--------|
| High (>80%) | Apply tags directly |
| Medium (50-80%) | Apply with `// TODO: verify tags` comment |
| Low (<50%) | Skip or flag for human review |

### Example Analysis

**Input file:** `internal/payment/stripe/client.go`
```go
package stripe

import (
    "context"
    "github.com/stripe/stripe-go/v74"
    "myapp/internal/payment"
)

type Client struct { ... }
func (c *Client) CreateCharge(ctx context.Context, req payment.ChargeRequest) (*payment.ChargeResponse, error) { ... }
func (c *Client) RefundCharge(ctx context.Context, chargeID string) error { ... }
```

**Analysis:**
- Path: `internal/payment/stripe` → domain: payment, specificity: stripe
- Import: `stripe-go` → external integration
- Import: `internal/payment` → implements payment interfaces
- Methods: `CreateCharge`, `RefundCharge` → payment operations

**Output:**
```go
// @focus: #payment { stripe, charges } #integration { stripe }
package stripe
```

## Project Setup Checklist

1. **Define Group Vocabulary**
   Create `FOCUS_TAGS.md` in project root documenting:
    - All valid groups with descriptions
    - Common tags per group
    - Tagging conventions specific to project

2. **Tag Core Files First**
   Start with:
    - Entry points (`main` packages)
    - Shared types and interfaces
    - Core utilities

3. **Tag by Feature**
   For each major feature:
    - Identify all participating files
    - Apply consistent group
    - Use specific tags for sub-concerns

4. **Review and Refine**
    - Use focus-catalog to test selections
    - Verify cross-cutting selections work
    - Adjust vocabulary as patterns emerge

5. **Maintain**
    - Tag new files at creation
    - Update tags during refactoring
    - Periodic review for consistency

## Quick Reference Card

```
SYNTAX
// @focus: #group { tag1, tag2 }

SPECIAL
#all                    Always include file

PLACEMENT
Before package statement
After build tags

LIMITS
2-3 groups per file
2-4 tags per group

NAMING
Groups: lowercase noun
Tags: lowercase, descriptive

COMMON GROUPS
#core      Fundamentals
#api       HTTP/gRPC layer
#data      Persistence
#auth      Security
#cmd       Entry points
#util      Utilities
#test      Test files

SELECTION TIPS
- Tag by concern, not location
- Multiple groups per file OK
- Granular > broad
- Consistent vocabulary
```