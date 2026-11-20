# Spec Delta

## Purpose

Provide a desktop-style z-Bot workspace with recognizable Open WebUI visual styling, accessible navigation, and room for future context tools.

## ADDED Requirements

### Requirement: Open WebUI desktop visual language

The shell SHALL closely match the inspected Open WebUI desktop visual language in light and dark themes: neutral surfaces, compact icon-and-label navigation, subtle separators, centered conversation, rounded composer, and a rounded settings dialog. It SHALL use Karta identity rather than Open WebUI branding.

#### Scenario: Desktop visual review
- **WHEN** the shell is reviewed at 1440 by 900 in both themes
- **THEN** its navigation density, surface hierarchy, typography, chat width, composer shape, and settings layout match the reference characteristics documented in the design
- **AND** the layout is a chat workspace rather than a dashboard of cards

### Requirement: Karta product identity

The visible app SHALL use Karta in the browser title, sidebar wordmark, assistant identity, composer accessible label, and settings identity. Its collapsed wordmark SHALL show K. Internal `zbot` identifiers, daemon contracts, and `~/.zbot/` paths SHALL remain unchanged by this visual rebrand.

#### Scenario: App identity across surfaces
- **WHEN** the user opens the app, collapses the left sidebar, views a reply, and opens Settings
- **THEN** the browser title and product labels identify Karta, with K shown in the collapsed rail
- **AND** no API or storage migration occurs

### Requirement: Independently collapsible workspace sidebars

The workspace SHALL provide left navigation, a central chat area, and a right context panel. Each sidebar SHALL have an accessible independent toggle. Unimplemented destinations and context tools SHALL display explicit TODO or coming-soon content rather than imply completed functionality.

#### Scenario: Toggle desktop sidebars
- **WHEN** a user collapses either sidebar and then expands it
- **THEN** the central chat uses the available space and the other sidebar's state and current conversation remain unchanged

#### Scenario: Open placeholder destination
- **WHEN** a user selects an unfinished Wards or configuration navigation destination, or opens the right context panel
- **THEN** the shell displays a named placeholder with its future purpose and an explicit not-implemented label
- **AND** no tool, filesystem, or daemon operation occurs

### Requirement: Responsive workspace

The shell SHALL keep the composer and main content usable from 375-pixel-wide screens through desktop sizes without page-level horizontal overflow. Below desktop sidebar breakpoints, sidebars SHALL become dismissible overlays rather than squeeze the conversation. Only one sidebar overlay SHALL be open at once.

#### Scenario: Narrow screen navigation
- **WHEN** the shell is used at 375 by 812 and a sidebar toggle is activated
- **THEN** an overlay opens with an accessible close control and backdrop dismissal
- **AND** closing it restores access and focus to the main workspace

#### Scenario: Return to desktop
- **WHEN** an open narrow-screen overlay crosses into desktop layout
- **THEN** stale backdrops, focus traps, and inert states are removed and the workspace remains operable

### Requirement: Accessible overlays and theme control

Settings and narrow-screen sidebar overlays SHALL have names, managed focus, Escape dismissal, and focus restoration. Controls SHALL expose accessible names, selection or expanded state, and visible keyboard focus. Theme switching SHALL work in memory for light and dark appearances and respect reduced-motion preferences.

#### Scenario: Keyboard settings flow
- **WHEN** a keyboard user opens Settings, moves through its controls, and presses Escape
- **THEN** focus remains within the dialog while open and returns to the opener on dismissal

#### Scenario: Theme preview
- **WHEN** the user switches themes
- **THEN** workspace, settings, placeholders, and interactive states use the selected theme without a network request or persistent write

### Requirement: Daemon-independent local preview

The shell SHALL start and function without a daemon or credentials. Runtime state SHALL stay in memory; mock surfaces SHALL NOT use browser persistence, telemetry, external assets, provider requests, daemon HTTP/SSE, or filesystem writes. Fixture content SHALL be clearly identified as mock data, and settings sections that are live daemon views SHALL degrade to a visible daemon-connection state without fixture fallback.

#### Scenario: Offline preview
- **WHEN** the built shell is served locally with no daemon and external network access blocked
- **THEN** navigation, mock chat, themes, and settings remain usable using only local static assets
- **AND** reloading resets UI state and no localStorage, sessionStorage, IndexedDB, or application cookies have been written
