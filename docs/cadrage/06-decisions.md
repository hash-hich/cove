# Décisions et risques acceptés

*Cadrage §8–§9 — index : [README.md](README.md).*

## 8. Conditions de l'acceptation du risque GitLab

À vérifier une fois, puis à re-vérifier si l'organisation du projet change.

**Fermeture de S10.** Le wrapper pousse les branches d'agent avec
`--push-option=ci.skip` (`-o ci.skip` en git ≥ 2.18). C'est un contrôle appliqué
**au push par le wrapper**, qui est fiable : l'agent ne pousse jamais lui-même et
ne peut donc pas le retirer. Le contraire — un `workflow:rules:` dans
`.gitlab-ci.yml` — vivrait dans le dépôt, sous contrôle de l'agent (S06/S09), et
ne convient pas. Un push `ci.skip` crée un pipeline marqué *skipped* qui
n'exécute aucun job ; combiné à la condition 2 ci-dessous (branche non protégée),
aucune exécution CI n'a lieu avant que l'owner ne lance le pipeline
manuellement, après relecture — moment où R3 gouverne.

*Vérification* : pousser une branche d'agent via le wrapper ne crée aucun
pipeline exécutant un job.

Conditions résiduelles, pour le pipeline que l'owner lance **délibérément** après
relecture :

1. Toutes les variables sensibles sont marquées **Protected**. Attention : *Masked*
   n'est pas *Protected* — le masquage ne fait que caviarder les logs, le job lit
   quand même la valeur.
2. Aucun motif de branche protégée ne couvre les branches d'agent (condition dont
   dépend aussi la fiabilité de `ci.skip`).
3. L'allowlist de `CI_JOB_TOKEN` est connue. Résidu assumé : minutes CI
   consommées, et publication possible dans le registry de paquets du projet.

Note : l'agent n'ayant aucune route vers GitLab, il ne peut pas lire le
résultat de son propre pipeline. Pas de boucle de rétroaction.

## 9. Décisions

Les décisions ouvertes des versions précédentes sont tranchées ici. Deux restent
différées (D5, D6), pour les raisons indiquées.

| # | Décision | État |
|---|----------|------|
| D1 | Authentification de l'agent auprès de l'API Claude (R2, R9) | **Tranchée — broker autonome.** Un service local **indépendant du wrapper** (démon, cycle de vie propre) détient le jeton d'abonnement (`setup-token`) pour toute sa durée de vie. Deux faces (R9) : une **face de contrôle** locale (socket) où le wrapper frappe et révoque une **capability par run** (portée, budget, expiration) ; une **face de données**, cible de `ANTHROPIC_BASE_URL`, liée à la seule interface de la sandbox, où le Claude Code de la sandbox envoie ses requêtes en portant la capability via `ANTHROPIC_AUTH_TOKEN`. Le wrapper est **client** du broker, ne détient jamais le jeton réel, et ne détruit que la sandbox (R5) ; le broker survit au run. Le `setup-token` étant limité aux requêtes de modèle, l'écart de portée avec une clé Console scopée est faible ; le plafond de dépense manquant est fourni par le broker (R4, R9). **Validé** : le trafic authentique de Claude Code franchit l'authentification à travers un relais transparent qui ne réécrit que l'`Authorization`, flux SSE sans mise en tampon compris. Le verrou de signature existe bien (le jeton n'est honoré que pour un trafic reconnu comme Claude Code, « only authorized for use with Claude Code »), mais le relais transparent le franchit *par construction*, puisque c'est réellement Claude Code qui s'exécute dans la sandbox — le broker ne fait que transporter sa signature (en-têtes, user-agent) sans l'altérer. **Restent ouverts** : un run agentique multi-tours réel (dernière vérification de conformité, pas encore faite) ; le comptage d'usage en flux ; et la veille sur un durcissement éventuel de la détection amont, qui se manifesterait en `429` sans en-têtes `anthropic-ratelimit-*` (canari) et forcerait le repli sur clé Console scopée + workspace vide (S28). |
| D2 | L'historique git complet est-il acceptable dans la boîte (S15) ? | **Tranchée — oui.** L'historique complet entre dans la sandbox. Les secrets qui y traînent relèvent du risque accepté S15, cohérent avec S30 : la confidentialité du dépôt n'est pas protégée. Pas de clone superficiel. |
| D3 | Identité des commits produits par l'agent (S19) | **Tranchée — auteur dédié.** Auteur/committer git dédié (bot), jamais l'identité du propriétaire du poste. **Aucune signature avec une clé du poste** : signer les commits de l'agent détruirait la non-répudiation que S19 identifie. |
| D4 | Emplacement et format de la configuration du wrapper (S08) | **Tranchée — hors dépôt.** La configuration du wrapper vit hors du dépôt cible : S08 est fermé par absence (P2), le wrapper ne source rien du dépôt (R6). En revanche `CLAUDE.md`/`AGENT.md` vivent **dans** le dépôt comme entrée légitime de l'agent, lue dans la sandbox. Leurs modifications, comme `.claude/` et `.mcp.json`, sont traitées par R7′ (alerte pour l'instruction, blocage pour l'exécutable) car elles atteignent le poste à la prochaine session normale (S09). Partage tâche / régime (P0) : le `CLAUDE.md`/`AGENT.md` du dépôt porte le **non vérifiable** — ton, consignes, intention — libre et non fiable ; le **vérifiable** — schéma de réponse, accès, bornes — vit dans le prompt système injecté par le wrapper, souverain, et **prime en cas de conflit**. |
| D5 | Valeurs par défaut des plafonds R4 | **Différée — à définir en implémentant.** Les quatre plafonds (CPU/RAM, disque, durée, tokens) et leurs pendants côté récepteur et broker seront calibrés sur des runs réels. R4 et sa vérification restent l'exigence ; seules les valeurs sont différées. |
| D6 | Famille d'isolation | **Tranchée — micro-VM native, Apple `container`.** Sur un hôte macOS Apple Silicon, `container` (1.0, adossé à Virtualization.framework) donne une micro-VM par run : R1 et R5 par **absence** (le `$HOME` du poste n'est pas monté — P2), R4 nativement (CPU/RAM/disque comptés par VM), frontière à l'**hyperviseur** et non au noyau partagé (§10, annexe). Images OCI standard → l'environnement de dev vient d'une image dont on contrôle exactement le contenu (rien du poste). **Contrainte** : macOS 26 + Apple Silicon. **Repli** si le poste n'est pas sur macOS 26 : Tart ou Lima (mêmes fondations Virtualization.framework, micro-VM). **Pourquoi pas Firecracker**, pourtant la référence micro-VM côté serveur : il pilote KVM et exige un hôte Linux ; sur Mac il ne tourne qu'imbriqué dans une VM Linux (deux couches d'hyperviseur, une VM Linux permanente à entretenir) pour le **même** modèle d'isolation, et ses mainteneurs ne prévoient pas de support macOS. Firecracker redevient le bon choix si la sandbox migre un jour sur un runner Linux. |
| D7 | Confidentialité du dépôt (S30) | **Tranchée — risque accepté.** Le code et la spec sont exfiltrables ; on l'accepte pour garder P1 (internet gratuit). **Condition de révision** : si un dépôt contenant du code confidentiel entre dans le périmètre, P1 est réévalué — allowlist réseau stricte ou coupure du canal sortant — au prix de l'ergonomie. |
| D8 | Plan de contrôle — comment un run est déclenché | **Tranchée — aucune surface réseau entrante sur le wrapper.** Le wrapper est un binaire qu'on invoque (CLI), pas un service qui écoute ; le broker n'a qu'un socket local et une face de données vers la sandbox. Le déclenchement distant (webhooks GitLab/GitHub) vit dans un **projet amont dédié**, séparé, dont c'est le seul rôle : il porte la surface réseau entrante, la vérification de signature du webhook, l'identité de forge et les plafonds (fréquence, concurrence), et n'a qu'un droit sur le wrapper — l'**invoquer**, par tableau d'arguments validés (jamais une ligne de commande concaténée, R6). Le contenu d'un webhook est une **entrée hostile** (quiconque ouvre une MR le déclenche) : l'amont ne passe au wrapper que des données structurées et validées (dépôt, SHA, demandeur), jamais du texte libre recopié. Cette séparation garde au wrapper sa propriété « aucune porte » et rend le déclencheur remplaçable (Slack, autre forge) sans y toucher. |
