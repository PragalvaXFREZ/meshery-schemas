# <a name="contributing">Contributing to Meshery Schemas</a>

## 👋 Welcome!

Thank you for your interest in contributing to Meshery Schemas — we're thrilled to have you here! Whether you're fixing a typo, proposing a new schema, or diving deep into code generation, every contribution matters and is genuinely appreciated.

Meshery Schemas is the central repository for all schema definitions used across Meshery's components. It follows a schema-driven development model where OpenAPI schemas are used to auto-generate Go structs, TypeScript types, and API clients.

## 📚 Detailed Contributing Guidelines

For comprehensive, step-by-step instructions on schema-driven development — including how to create and modify schemas, understand the build pipeline, and follow project conventions — please visit:

### 👉 [docs.meshery.io](https://docs.meshery.io)

You'll find detailed guides covering:
- Schema structure and conventions
- Code generation workflow (`make build`)
- What to commit (and what not to)
- OpenAPI best practices for this project

## 🚀 Quick Start

```bash
make setup && npm install   # install dependencies
make build                  # generate Go, TypeScript, and RTK code
npm run build               # build the TypeScript distribution
```

## 🤝 Getting Help

- [GitHub Issues](https://github.com/meshery/schemas/issues) - report bugs or request features
- [Community Slack](https://slack.meshery.io) - chat with maintainers and contributors
- [Weekly Meetings](https://meshery.io/community/calendar) - join our community calls

---

> For general Meshery contribution guidelines, see the [Meshery Contributing Guide](https://github.com/meshery/meshery/blob/master/CONTRIBUTING.md).
