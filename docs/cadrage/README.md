# Cadrage — exécution d'un agent de code non supervisé

**Statut : cadrage. L'architecture est actée (décisions §9) ; les premiers choix
d'implémentation sont consignés (§11), le reste ne l'est pas.**
Le cadrage définit le périmètre, l'adversaire, les surfaces d'attaque et les
exigences ; le reste en est déduit, pas décidé ici. Le mode autonome (post-MVP)
est conçu au §12.

Ce répertoire est le cadrage découpé en documents courts, à lire **à la demande**
selon la tâche — pas en bloc. Les numéros de section (§1 à §12) et les
identifiants stables sont conservés : « §9 », « R8 » ou « S26 » désignent
toujours la même chose, quel que soit le fichier.

## Carte

| Sections | Fichier | Contenu | À lire quand… |
|---|---|---|---|
| §1–§4 | [01-probleme-et-perimetre.md](01-probleme-et-perimetre.md) | Le problème, les quatre composants, ce qu'on protège ou non, le modèle d'adversaire | on touche au périmètre, on ajoute un composant ou un flux |
| §5 | [02-surfaces.md](02-surfaces.md) | Catalogue des surfaces S01–S31 et leur traitement | on évalue un nouveau risque ou on cite une surface |
| §6 | [03-principes.md](03-principes.md) | Principes P0–P6 : les invariants qui tranchent les arbitrages | un arbitrage n'est couvert par aucune exigence |
| §7 (R1–R5) | [04-exigences-sandbox.md](04-exigences-sandbox.md) | Ce que la boîte voit, ne contient pas, ce qui la borne et la détruit | on travaille sur l'image, les montages, les plafonds, le teardown |
| §7 (R6–R9) | [05-exigences-frontiere.md](05-exigences-frontiere.md) | Wrapper, inspection de la sortie, canal de récupération, broker ; procédure d'acceptation « agent piégé » | on travaille sur le wrapper, le push, l'inspection, le broker, les tests d'acceptation |
| §8–§9 | [06-decisions.md](06-decisions.md) | Conditions du risque GitLab (`ci.skip`) et décisions D1–D8 | on se demande si un choix est acté, ou avant d'en rouvrir un |
| §10–§11, annexe | [07-implementation.md](07-implementation.md) | Contraintes induites, notes d'implémentation (Apple `container`, Go, topologie du broker), familles d'isolation | on choisit une technologie ou une structure de code |
| §12 | [08-mode-autonome.md](08-mode-autonome.md) | Régime autonome post-MVP : états terminaux, reprise, fil de MR comme session | on conçoit le mode sans humain dans la boucle |

## Identifiants stables

Un identifiant est attribué une fois et ne bouge plus, même si l'élément est
reclassé (d'où des numéros non séquentiels).

- **S01–S31** — surfaces d'attaque (§5).
- **P0–P6** — principes directeurs (§6).
- **R1–R9** — exigences, chacune avec son critère de vérification (§7). « R7′ »
  a remplacé un R7 plus étroit ; les autres numéros n'ont pas bougé.
- **D1–D8** — décisions d'architecture (§9) ; D5 est différée.

## Sources

- [Claude Code — Authentication](https://code.claude.com/docs/en/authentication) : `claude setup-token`, portée du jeton (« It can only make model requests »), ordre de précédence des credentials, `ANTHROPIC_AUTH_TOKEN` et `ANTHROPIC_BASE_URL`
- Restriction du jeton d'abonnement à un usage *avec Claude Code* (rejet « only authorized for use with Claude Code » hors de ce contexte) — à revérifier avant implémentation du broker (D1, risque résiduel n°1)
- [Files API — scoping et accès](https://platform.claude.com/docs/en/build-with-claude/files)
- [Admin API — clés et permissions](https://platform.claude.com/docs/en/manage-claude/admin-api)
- GitLab — option de push `ci.skip` (`git push -o ci.skip`, git ≥ 2.18 ; `--push-option=ci.skip` depuis git 2.10) : crée un pipeline marqué *skipped* sans exécuter de job ; contrôle côté pousseur, non défait par un fichier du dépôt
- Apple `container` — 1.0.0 (9 juin 2026), Apache-2.0, macOS 26 / Apple Silicon : une micro-VM légère par conteneur OCI, adossée à Virtualization.framework ; frontière d'isolation à l'hyperviseur (D6, §10, annexe)
- Firecracker — pilote KVM et exige un hôte Linux exposant `/dev/kvm` ; pas de support macOS (position des mainteneurs) ; sur Apple Silicon uniquement en imbriqué dans une VM Linux (M3+/macOS 15+ pour la virtualisation imbriquée)
