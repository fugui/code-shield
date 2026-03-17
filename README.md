# Code-Shield 🛡️ (formerly Code-Reporter)

An intelligent, autonomous AI-assisted code review and key-issue tracking system designed to guard your code quality. Code-Shield operates as a guardian (like a digital Code Gardener), dynamically scanning branches, employing LLMs for deep architectural reviews, and distributing proactive notifications to the responsible individuals.

## Core Features
- **Repository & Team Management**: Centralized dual-tab management system for defining departments, personnel, and assigning Git repositories to owners.
- **Key Issue Tracking**: Direct mapping of known architectural debts or bugs to responsible team members.
- **Automated AI Review Engine**: Trigger automated deep-dive reviews on any repository branch. The system physically pulls the codes, interfaces with cutting-edge LLMs (via Claude CLI), and generates comprehensive review artifacts.
- **Live Reports**: Fully asynchronous review pipeline with real-time UI polling ("Pending" → "Success") and integrated markdown-rendered report sidebars.
- **Real-time Notifications**: Background Node.js notifier dispatching alerts to members when the AI detects anomalies.

## Tech Stack
- Frontend: React + Vite + Custom Components
- Backend: Go (Gin) + GORM (SQLite)
- Orchestration: Makefiles + Node Events

## Getting Started
```bash
# 1. Install necessary JS dependencies
cd shield-server/frontend && npm install
cd ../../notifier && npm install

# 2. Build frontend assets and run the full stack
cd ..
make build
make run
```
