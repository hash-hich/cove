# 5. Catalogue des surfaces

Identifiants stables, à citer dans les discussions ultérieures. Un identifiant
est attribué une fois et ne bouge plus, même si la surface est reclassée — d'où
des numéros non séquentiels par catégorie (S26–S31 dispersés).

## A — Exfiltration de données

| ID | Surface | Traitement |
|----|---------|-----------|
| S01 | DNS : encodage de données dans une requête de résolution, survit à un filtrage HTTP | R2 |
| S02 | Les commits et la MR eux-mêmes : canal sortant par construction | R2 + §3.2 |
| S03 | Le log : le `tee` tourne hors sandbox et écrit sur le disque | R6 |
| S04 | Domaines autorisés : filtrage par domaine, aucune inspection de contenu | R2 |
| S05 | Tout chemin inscriptible synchronisé (iCloud, Dropbox, sauvegarde continue) est un canal réseau déguisé | R1, R5 |
| S30 | **Confidentialité du dépôt** : le code source et la spec de la tâche vivent dans la sandbox ; avec un canal sortant, un agent hostile les exfiltre. P1 rend le canal sortant gratuit, donc rien ne l'en empêche | Risque accepté (§9, D7) |

## B — Exécution hors de la boîte

| ID | Surface | Traitement |
|----|---------|-----------|
| S06 | Le clone local, **avant tout merge** : `.envrc` (direnv au `cd`), `.vscode/tasks.json` au folderOpen, éditeur, indexeur | R8, R7′ |
| S07 | Chaîne d'approvisionnement tirée par l'agent : dépendance typosquattée ou compromise, invisible dans un lockfile | §3.2 + R7′ (alerte) |
| S08 | **La configuration du wrapper**, si elle est du shell sourcé depuis le dépôt cible. Ne nécessite aucun merge : un `checkout` pour relire suffit | Fermé par absence (D4 : config hors dépôt) + R6 |
| S09 | `.claude/`, `CLAUDE.md`, `AGENT.md`, `.mcp.json` dans le dépôt : s'exécutent ou orientent la prochaine session normale, hors sandbox | R7′ (bloque l'exécutable, alerte l'instruction) |
| S10 | Pipeline CI déclenché au push, avant toute relecture ; le statut Draft ne le bloque pas | Fermé par `ci.skip` côté wrapper (§8) |
| S11 | Liens symboliques sortant du périmètre, suivis plus tard par une copie, un build ou un archivage | R6 |
| S12 | Gatekeeper ne s'applique pas : un binaire écrit par l'agent n'a pas l'attribut de quarantaine | Documenté, §3.2 |
| S26 | **Références `refs/replace/`** : substituent un objet par un autre de façon transparente dans toutes les commandes git. Le diff relu peut différer du contenu poussé. Non récupérées par le refspec par défaut, ramenées par `refs/*`, `--tags` ou un mirror | R8 |
| S27 | Sous-modules : un `.gitmodules` récupéré peut porter une URL `ext::sh -c …`, exécutée à la récursion ou au `submodule update` | R8 |
| S32 | **Réseau local et autres VMs joignables depuis la sandbox** : le réseau `default` de `container` est un NAT sans filtrage ; l'invité atteint le routeur, tout service du LAN, l'adresse LAN de l'hôte et les autres VMs du même réseau (D10). Un agent piégé peut donc parler à une imprimante, un NAS, l'interface d'administration du routeur ou au run concurrent. Internet reste ouvert par défaut (P1) : la doc et les paquets sont des entrées légitimes | Canal entre runs : fermé par un réseau par run (D10). LAN : risque non accepté ; `container` 1.3.1 n'offre ni filtrage ni réseau sans LAN qui garde l'hôte joignable. Traitement proposé, vérifié en D10 : plages privées rejetées depuis l'invité par nftables, règles posées en root avant le lancement de l'agent, qui ne peut ni les lire ni les défaire ; internet et passerelle ouverts ; test de fraîcheur à chaque run (P4). Exige `--cap-add NET_ADMIN` au lancement et `nftables` dans l'image (D9, issue de suivi) |

## C — Lecture et confidentialité

| ID | Surface | Traitement |
|----|---------|-----------|
| S13 | Lecture du disque : les sandboxes OS autorisent la lecture par défaut. Clés SSH, credentials cloud, `.env` de tous les projets | R1 |
| S14 | `~/.claude` : transcripts de **toutes** les autres sessions, plus le jeton d'accès | R1, R2 |
| S15 | L'historique git du dépôt : les secrets supprimés y sont toujours | Accepté (§9, D2) |
| S16 | L'URL du remote, si elle embarque un token | R2 |
| S28 | **Portée de l'identifiant Claude** — dépend entièrement du type d'identifiant, voir R2 : une clé Console non scopée atteint l'Admin API ; une clé scopée donne accès à tous les fichiers du workspace ; un jeton `setup-token` est limité aux requêtes de modèle mais reste portable et non plafonnable | R2 |

## D — Attaques contre l'humain

| ID | Surface | Traitement |
|----|---------|-----------|
| S17 | Séquences d'échappement ANSI dans le log, rendues dans le terminal | R6 |
| S18 | Résumé mensonger, diff illisible, caractères bidi (Trojan Source), code planqué dans du vendored | R7′ |
| S19 | Identité : commits attribués — voire signés — au propriétaire du poste. Problème de non-répudiation | R2 + §9, D3 |
| S20 | Les outils hors sandbox parsent des données hostiles : `git`, `less`, l'éditeur, Spotlight, QuickLook, le prompt shell qui lance `git status` | R6, R8 |
| S31 | **Rendu de la MR chez le relecteur** : le titre, la description et le résumé de la MR sont rédigés par l'agent et rendus en Markdown dans le navigateur. Un lien externe cliquable (phishing ciblé, exfiltration par paramètres d'URL), une image distante servant de balise (révèle l'IP et l'instant de la relecture) | R6 + R7′ |

## E — La garde elle-même

| ID | Surface | Traitement |
|----|---------|-----------|
| S21 | Fail-open silencieux : une clé de configuration renommée par une mise à jour, et la règle cesse de s'appliquer sans erreur | §7 (vérification) |
| S22 | État partagé entre runs : canal entre deux agents, accumulation entre deux exécutions | R5, R9 |
| S29 | **Le broker** : service côté poste, **autonome et à cycle de vie propre** (non lancé par le wrapper), qui détient l'identifiant réel et parse en flux (relais SSE) des requêtes rédigées par l'adversaire. Bug de parsing HTTP, désérialisation qui exécute, mise en tampon mal gérée, fuite du jeton réel vers la sandbox — chacun s'exécute côté poste avec l'identifiant à portée | R9 (R6 appliqué au broker) |

## F — Déclencheur et bruit de fond

| ID | Surface | Traitement |
|----|---------|-----------|
| S23 | Prompt injection | Hypothèse du §4, pas une surface à traiter |
| S24 | Épuisement de ressources : disque, CPU, quota de tokens — y compris côté récepteur (bombe de fetch) et côté broker | R4, R8, R9 |
| S25 | Évasion de l'isolation | Hors sujet (§3.3) |
