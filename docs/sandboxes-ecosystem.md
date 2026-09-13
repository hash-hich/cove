# Sandboxes et outillage d'agents de codage

Relevé de septembre 2026. Les descriptions viennent des sources citées, pas d'une mise à l'épreuve.

## Agents de codage et leur sandbox intégrée

| Nom | Description |
|---|---|
| [openai/codex](https://github.com/openai/codex) | CLI d'agent, sandbox active par défaut : Seatbelt, Landlock et seccomp, jetons Windows. Modes read-only, workspace-write, danger-full-access |
| Codex cloud | Pendant hébergé de Codex CLI, conteneurs sandboxés côté OpenAI |
| Claude Code | Bash sandboxé par `@anthropic-ai/sandbox-runtime`, sessions web en micro VM complètes |
| Gemini CLI | Seatbelt sur macOS ou conteneur Docker et Podman via `.gemini/sandbox.Dockerfile` |
| GitHub Copilot Coding Agent | Une VM de sandbox éphémère par tâche |
| Google Jules | Agent asynchrone exécuté en VM Cloud |
| Cursor, Devin | Sandbox ou VM hébergée par tâche, VM dédiées pour Devin |
| Kiro (AWS) | Agent avec son propre environnement d'exécution |
| Replit Agent | Exécution dans la VM ou le conteneur Replit |
| [OpenHands](https://github.com/All-Hands-AI/OpenHands) | Un conteneur Docker de runtime par session, plus un remote runtime hébergé |
| [SWE-agent](https://github.com/SWE-agent/SWE-agent) | Harness de recherche, une image Docker par instance de tâche |

## Machines virtuelles et micro VM

| Nom | Description |
|---|---|
| [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) | Une micro VM par sandbox, avec daemon Docker, système de fichiers et réseau propres. Claude Code, Codex, Gemini, Kiro |
| [microsandbox](https://github.com/microsandbox/microsandbox) | Micro VM libkrun, boot sous 200 ms, serveur MCP, mode éphémère `msx` |
| [cleanroom](https://github.com/buildkite/cleanroom) | Micro VM à egress deny par défaut et proxy de credentials |
| [Apple container](https://github.com/apple/container) | Exécuteur de conteneurs en micro VM sur Apple Silicon |
| [k7](https://github.com/katakate/k7) | Infra de sandboxes VM légères self hosted, API et SDK |
| [BoxLite](https://github.com/boxlite-ai/boxlite) | Sandbox VM embarquable avec snapshots |
| [smolVM](https://github.com/smol-machines/smolvm) | Gestionnaire de micro VM locales |
| [Chamber](https://github.com/cirruslabs/chamber) | VM macOS éphémère via Tart, pour Claude Code et Codex |
| [ClodPod](https://github.com/webcoyote/clodpod) | VM projetant les projets de l'hôte dans l'invité |
| [lima-devbox](https://github.com/recodelabs/lima-devbox) | Sandbox de dev sur Lima, VM Linux sous macOS |
| [agentsafe](https://github.com/sarthak30/agentsafe) | Une micro VM Firecracker par tâche |
| [nervos](https://github.com/ashishgituser/nervos) | Une micro VM Firecracker par tâche |
| Windows Sandbox | VM jetable Hyper-V livrée avec Windows Pro |
| WSL2 | Frontière VM de fait sur Windows |
| bunkervm | Petite VM Linux pour agents, dépôt supprimé |

## Runtimes conteneur et SDK d'exécution

| Nom | Description |
|---|---|
| [AIO Sandbox](https://github.com/agent-infra/sandbox) | Image Docker réunissant shell, navigateur, fichiers, Jupyter, VS Code, MCP |
| [Kilntainers](https://github.com/Kiln-AI/Kilntainers) | Runtime MCP sur Docker, Podman, micro VM ou Wasm |
| [forgemax](https://github.com/postrv/forgemax) | Passerelle MCP avec exécution de code sandboxée |
| [coderunner](https://github.com/instavm/coderunner) | Exécuteur de code IA non fiable, auto-hébergé |
| [vibebin](https://github.com/jgbrwn/vibebin) | Plateforme persistante sur Incus et LXC |
| [Microbox](https://github.com/hqarroum/microbox) | Sandboxes Linux éphémères légères |
| [Piston](https://github.com/engineer-man/piston) | Moteur d'exécution de code non fiable, antérieur à la vague agent |
| [Judge0](https://github.com/judge0/judge0) | API d'exécution de code isolée, très largement déployée |
| kubernetes-sigs/agent-sandbox | Sandboxes d'agents sur Kubernetes. À vérifier, source incertaine |

## Lanceurs multi backend

| Nom | Description |
|---|---|
| [cco](https://github.com/nikvdp/cco) | Lanceur mince qui choisit un backend de sandbox local |
| [yoloAI](https://github.com/kstenerud/yoloai) | Exécuteur au dessus de Seatbelt, Tart ou Docker |
| [boxed](https://github.com/akshayaggarwal99/boxed) | Moteur d'exécution à backends Docker, Firecracker ou WASM |

## Sandboxes au niveau du système

| Nom | Description |
|---|---|
| [sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime) | Le `srt` d'Anthropic : Seatbelt ou bubblewrap, plus proxy de filtrage réseau |
| [Agent Safehouse](https://github.com/eugene1g/agent-safehouse) | Profils Seatbelt en deny first |
| [sandbox-shell](https://github.com/agentic-dev3o/sandbox-shell) | Enveloppe de shell sous Seatbelt |
| [SandVault](https://github.com/webcoyote/sandvault) | Compte utilisateur macOS séparé, durci par `sandbox-exec` |
| [vibebox](https://github.com/robcholz/vibebox) | Sandbox Seatbelt locale rapide |
| [yolobox](https://github.com/finbarr/yolobox) | Sandbox Seatbelt locale rapide |
| [Fence](https://github.com/use-tusk/fence) | Sandbox de commandes native, sans conteneur |
| [Matchlock](https://github.com/jingkaihe/matchlock) | Sandbox Linux pour agents de codage |
| [Nono](https://github.com/always-further/nono) | Sandbox orientée capacités, adossée au noyau |
| [sandbox-run](https://codeberg.org/Grauwolf/sandbox-run) | Enveloppe bubblewrap déclarée par projet |
| [shai](https://github.com/colony-2/shai) | Shell de sandboxing pour agents |
| [sucoder](https://github.com/ligon/sucoder) | Confinement par les seules permissions Unix |
| [Greywall](https://github.com/greyhavenhq/greywall) | Sandbox locale à contrôles réseau modifiables à chaud |
| [agent-win-sandbox](https://github.com/fmuecke/agent-win-sandbox) | Utilisateur Windows restreint, agnostique de l'agent, outils de build natifs conservés |
| [OpenShell](https://github.com/NVIDIA/OpenShell) | NVIDIA, nature de la frontière non documentée dans la source |

## Orchestrateurs multi agents

| Nom | Description |
|---|---|
| [fletch](https://github.com/fwdai/fletch) | App macOS, `git clone --shared` par agent sous Seatbelt ou Docker, push et PR côté hôte |
| [agentbox](https://github.com/madarco/agentbox) | CLI d'agents parallèles, Docker overlay FUSE ou VM cloud, démarrage sous la seconde |
| [container-use](https://github.com/dagger/container-use) | Worktrees conteneurisés par agent en MCP, revue par branche git |
| [Sculptor](https://github.com/imbue-ai/sculptor) | UI de bureau pour agents en conteneurs isolés |
| [Conductor](https://conductor.build/) | App Mac, Claude Code et Codex en parallèle dans des worktrees isolés |
| [treebeard](https://github.com/divmain/treebeard) | Worktree git éphémère en copie sur écriture, réseau conditionnel |
| [packnplay](https://github.com/obra/packnplay) | Sandbox de commandes Docker organisée autour de worktrees |

## Wrappers autour d'un agent précis

| Nom | Description |
|---|---|
| [claude-code/.devcontainer](https://github.com/anthropics/claude-code/tree/main/.devcontainer) | Devcontainer officiel avec pare-feu d'egress en allowlist |
| [claude-contained](https://github.com/emarc/claude-contained) | Lanceur Docker ou Apple Container avec état persistant, pour Claude, Codex, Gemini, Vibe |
| [ClaudeBox](https://github.com/RchGrav/claudebox) | Environnement Docker pour Claude Code avec allowlists |
| [sandclaude](https://github.com/binwiederhier/sandclaude) | Enveloppe Docker opinionée pour Claude Code |
| [claude-code-devcontainer](https://github.com/trailofbits/claude-code-devcontainer) | Gabarit de devcontainer durci, Trail of Bits |
| [codex-lockbox](https://github.com/paulux84/codex-lockbox) | Sandbox Docker et règles de pare-feu pour Codex CLI |
| codex-container-sandbox | Enveloppe Podman n'exposant que le dépôt et des montages désignés |
| [agentbox](https://github.com/gbrindisi/agentbox) | Sandbox conteneurisée avec abandon de privilèges et pare-feu |
| [EdgeBox](https://github.com/bigppwong/edgebox) | Sandbox locale exposant un bureau graphique à l'agent |

## Plateformes d'environnements de développement

| Nom | Description |
|---|---|
| [coder/coder](https://github.com/coder/coder) | Environnements de dev distants provisionnés par Terraform, self hosted |
| [warp](https://github.com/warpdotdev/warp) | Terminal devenu environnement de dev agentique, agents en parallèle |
| [DevPod](https://github.com/loft-sh/devpod) | Provisionneur de devcontainers open source, multi infrastructures |
| [Daytona](https://github.com/daytonaio/daytona) | Workspaces persistants, conteneurs OCI avec Kata ou Sysbox en option |
| Gitpod Flex, GitHub Codespaces | CDE hébergés adoptés comme sandboxes d'agents |

## Policy, approbation, audit

| Nom | Description |
|---|---|
| [LlamaFirewall](https://github.com/meta-llama/PurpleLlama) | Meta : PromptGuard, CodeShield, AlignmentCheck |
| [NeMo Guardrails](https://github.com/NVIDIA/NeMo-Guardrails) | Garde-fous programmables NVIDIA |
| [Docker MCP Gateway](https://github.com/docker/mcp-gateway) | Serveurs MCP en conteneurs avec gestion des secrets |
| [Invariant Gateway](https://github.com/invariantlabs-ai/invariant-gateway) | Passerelle d'interception et de garde des appels d'outils MCP |
| [Cupcake](https://github.com/eqtylab/cupcake) | Règles OPA et Rego appliquées sur les hooks |
| [immunity-agent](https://github.com/PrismorSec/immunity-agent) | Hooks PreToolUse et PostToolUse, flux de menaces signé, bloque, avertit ou masque |
| [nah](https://github.com/manuelschipper/nah) | Garde déterministe autoriser, demander, bloquer |
| [predicate-secure](https://github.com/PredicateSystems/predicate-secure) | Autorisation par politique et vérification après exécution |
| [claude-rule-enforcer](https://github.com/tech-and-ai/claude-rule-enforcer) | Règles de comportement imposées à l'agent |
| [shannot](https://github.com/corv89/shannot) | Circuit d'approbation avec humain dans la boucle |
| [punkgo-jack](https://github.com/PunkGo/punkgo-jack) | Audit et reçus des évènements de hooks, journal en arbre de Merkle |
| [deepclause-sdk](https://github.com/deepclause/deepclause-sdk) | SDK d'autorisation à l'exécution, style DML |

## Secrets et credentials

| Nom | Description |
|---|---|
| [segmentio/chamber](https://github.com/segmentio/chamber) | CLI Go de gestion de secrets sur AWS SSM Parameter Store, injection en variables d'environnement |

## Indexation de code pour agents

| Nom | Description |
|---|---|
| [codegraph](https://github.com/colbymchenry/codegraph) | Graphe de connaissance du code pré-indexé, resynchronisé à chaud, 20 langages |

## Listes et catalogues

| Nom | Description |
|---|---|
| [gist wincent](https://gist.github.com/wincent/2752d8d97727577050c043e4ff9e386e) | Catalogue des sandboxes d'agents, relevé de mai 2026, avec comparatif des plateformes |
| [restyler/awesome-sandbox](https://github.com/restyler/awesome-sandbox) | Taxonomie détaillée et cadre de décision |
| [webcoyote/awesome-AI-sandbox](https://github.com/webcoyote/awesome-AI-sandbox) | Liste open source curatée |
| [bureado/awesome-agent-runtime-security](https://github.com/bureado/awesome-agent-runtime-security) | Angle sécurité à l'exécution |
