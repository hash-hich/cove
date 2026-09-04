# Cadrage — exécution d'un agent de code non supervisé

**Statut : cadrage. L'architecture est actée (décisions §9) ; les premiers choix
d'implémentation sont consignés (§11), le reste ne l'est pas.**
Ce document définit le périmètre, l'adversaire, les surfaces d'attaque et les
exigences ; le reste en est déduit, pas décidé ici. Le mode autonome (post-MVP)
est conçu au §12.

---

## 1. Le problème

On veut faire travailler Claude Code sur un dépôt, **sans validation manuelle
pendant l'exécution** (`--dangerously-skip-permissions`), et récupérer le
résultat sous forme de Merge Request GitLab à relire.

Un agent qui exécute des commandes arbitraires en lisant du texte non fiable
(le contenu du dépôt, une page de documentation, un message d'erreur, le README
d'un paquet) doit être traité comme **de l'exécution de code non fiable**. La
question n'est pas « Claude va-t-il mal se comporter » mais « que se passe-t-il
si du code hostile s'exécute avec les privilèges de l'agent ».

Objectif du projet : **rendre l'environnement de l'agent inoffensif pour le
Mac**, à un coût d'ergonomie acceptable.

## 2. Les composants

Quatre composants ; seule la sandbox n'est pas sur le poste.

- **Le wrapper** — un binaire qu'on invoque (CLI), côté hôte. Il orchestre le run
  et *est* la frontière de sécurité (P6) : il provisionne la sandbox, y injecte la
  tâche, récupère et inspecte la sortie, pousse la MR. Aucune surface réseau
  entrante (D8).
- **La sandbox** — une micro-VM par run (D6) où s'exécute Claude Code. Elle ne
  voit que le dépôt, rien du poste ; elle est détruite à la fin (R5).
- **Le broker** — un démon autonome côté poste (D1) qui détient l'unique
  credential réel et relaie les appels de modèle de la sandbox vers Anthropic. Le
  wrapper en est le *client*, pas le parent.
- **L'amont de déclenchement** (post-MVP, D8) — un projet séparé qui traduit un
  webhook de forge en invocation du wrapper ; il porte la seule surface réseau
  entrante.

Deux flux à ne pas confondre : les **appels de modèle** vont sandbox → broker →
Anthropic ; la **récupération** va sandbox → wrapper → inspection → MR (objets
git, R8). Le broker n'est **pas** sur le second.

## 3. Périmètre

### 3.1 Ce qu'on protège

- Le poste de travail (fichiers, secrets, identité, ressources).
- Les identifiants qui y vivent : GitLab, Claude, SSH, cloud, tout le reste.
- L'intégrité des composants qui gardent la boîte : le wrapper (frontière, P6),
  le broker (service autonome détenant le credential, §9 D1) et le code de
  récupération et d'inspection.

Ce qu'on **ne** protège pas au titre de la confidentialité : le contenu du dépôt
lui-même. Il entre dans la sandbox par construction et un agent hostile disposant
d'un canal sortant peut l'exfiltrer. C'est un risque accepté explicite (S30, §9),
assorti d'une condition de révision.

### 3.2 Ce qu'on ne protège pas, délibérément

**Le code mergé.** Ce qui entre dans le dépôt relève de l'owner du dépôt, qui
relit chaque MR. Le rôle du projet s'arrête à garantir que rien n'atteint la
machine ou le CI **sans décision explicite** de sa part.

> Frontière exacte : *tout ce qui atteint mon Mac ou mon CI sans que je l'aie
> décidé est dans le périmètre ; ce que je merge sciemment n'y est pas.*
> Cette formulation place volontairement S06, S08 et S09 (voir §5) **dans** le
> périmètre : ils agissent avant, à côté, ou du fait même de la relecture.

**La confidentialité du code du dépôt.** Voir §3.1 et S30. Le projet protège le
poste et les identifiants, pas le secret du code que l'agent manipule.

**L'infrastructure GitLab.** Runners partagés gitlab.com (VM éphémères, sans
lien avec le poste) et variables sensibles marquées *Protected*. Le rayon de
souffle d'un pipeline sur une branche d'agent se limite à ce qui est déjà dans
le dépôt — et l'agent ne déclenche plus de pipeline avant relecture (S10, §8).
Risque accepté en connaissance de cause, sous les conditions du §8.

**La qualité du code produit.** Traitée par la spec d'entrée et une commande de
vérification, pas par ce document.

### 3.3 Hors sujet

Évasion d'hyperviseur, 0-day noyau, compromission d'Anthropic ou de GitLab. Un
adversaire disposant de ces moyens ne passera pas par une MR.

## 4. Modèle d'adversaire

**Hypothèse de travail : à l'intérieur de la sandbox, l'agent est hostile et
compétent.** Pas maladroit — hostile. Il a lu des instructions adverses et
poursuit un objectif qui n'est pas la tâche demandée.

**Vecteurs d'entrée** (aucun n'est bloquable, tous sont assumés) : contenu du
dépôt (README, commentaires, fixtures), pages web consultées pour la
documentation, sorties d'outils, métadonnées de paquets.

**Capacités supposées** : exécution arbitraire dans la sandbox, lecture de tout
ce qui y est visible, écriture de tout ce qui y est inscriptible, usage de tout
canal réseau ouvert, émission de requêtes arbitraires **vers le broker**, et
rédaction du contenu qui sort (commits, log, résumé de MR).

**Capacités non supposées** : évasion de l'isolation, accès à une machine tierce,
persistance après destruction de la sandbox, joignabilité du broker depuis une
autre machine que la sandbox.

## 5. Catalogue des surfaces

Identifiants stables, à citer dans les discussions ultérieures. Un identifiant
est attribué une fois et ne bouge plus, même si la surface est reclassée — d'où
des numéros non séquentiels par catégorie (S26–S31 dispersés).

### A — Exfiltration de données

| ID | Surface | Traitement |
|----|---------|-----------|
| S01 | DNS : encodage de données dans une requête de résolution, survit à un filtrage HTTP | R2 |
| S02 | Les commits et la MR eux-mêmes : canal sortant par construction | R2 + §3.2 |
| S03 | Le log : le `tee` tourne hors sandbox et écrit sur le disque | R6 |
| S04 | Domaines autorisés : filtrage par domaine, aucune inspection de contenu | R2 |
| S05 | Tout chemin inscriptible synchronisé (iCloud, Dropbox, sauvegarde continue) est un canal réseau déguisé | R1, R5 |
| S30 | **Confidentialité du dépôt** : le code source et la spec de la tâche vivent dans la sandbox ; avec un canal sortant, un agent hostile les exfiltre. P1 rend le canal sortant gratuit, donc rien ne l'en empêche | Risque accepté (§9, D7) |

### B — Exécution hors de la boîte

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

### C — Lecture et confidentialité

| ID | Surface | Traitement |
|----|---------|-----------|
| S13 | Lecture du disque : les sandboxes OS autorisent la lecture par défaut. Clés SSH, credentials cloud, `.env` de tous les projets | R1 |
| S14 | `~/.claude` : transcripts de **toutes** les autres sessions, plus le jeton d'accès | R1, R2 |
| S15 | L'historique git du dépôt : les secrets supprimés y sont toujours | Accepté (§9, D2) |
| S16 | L'URL du remote, si elle embarque un token | R2 |
| S28 | **Portée de l'identifiant Claude** — dépend entièrement du type d'identifiant, voir R2 : une clé Console non scopée atteint l'Admin API ; une clé scopée donne accès à tous les fichiers du workspace ; un jeton `setup-token` est limité aux requêtes de modèle mais reste portable et non plafonnable | R2 |

### D — Attaques contre l'humain

| ID | Surface | Traitement |
|----|---------|-----------|
| S17 | Séquences d'échappement ANSI dans le log, rendues dans le terminal | R6 |
| S18 | Résumé mensonger, diff illisible, caractères bidi (Trojan Source), code planqué dans du vendored | R7′ |
| S19 | Identité : commits attribués — voire signés — au propriétaire du poste. Problème de non-répudiation | R2 + §9, D3 |
| S20 | Les outils hors sandbox parsent des données hostiles : `git`, `less`, l'éditeur, Spotlight, QuickLook, le prompt shell qui lance `git status` | R6, R8 |
| S31 | **Rendu de la MR chez le relecteur** : le titre, la description et le résumé de la MR sont rédigés par l'agent et rendus en Markdown dans le navigateur. Un lien externe cliquable (phishing ciblé, exfiltration par paramètres d'URL), une image distante servant de balise (révèle l'IP et l'instant de la relecture) | R6 + R7′ |

### E — La garde elle-même

| ID | Surface | Traitement |
|----|---------|-----------|
| S21 | Fail-open silencieux : une clé de configuration renommée par une mise à jour, et la règle cesse de s'appliquer sans erreur | §7 (vérification) |
| S22 | État partagé entre runs : canal entre deux agents, accumulation entre deux exécutions | R5, R9 |
| S29 | **Le broker** : service côté poste, **autonome et à cycle de vie propre** (non lancé par le wrapper), qui détient l'identifiant réel et parse en flux (relais SSE) des requêtes rédigées par l'adversaire. Bug de parsing HTTP, désérialisation qui exécute, mise en tampon mal gérée, fuite du jeton réel vers la sandbox — chacun s'exécute côté poste avec l'identifiant à portée | R9 (R6 appliqué au broker) |

### F — Déclencheur et bruit de fond

| ID | Surface | Traitement |
|----|---------|-----------|
| S23 | Prompt injection | Hypothèse du §4, pas une surface à traiter |
| S24 | Épuisement de ressources : disque, CPU, quota de tokens — y compris côté récepteur (bombe de fetch) et côté broker | R4, R8, R9 |
| S25 | Évasion de l'isolation | Hors sujet (§3.3) |

## 6. Principes directeurs

Les invariants de conception. Ils tranchent les arbitrages que les exigences
ne couvrent pas explicitement. P0 les chapeaute.

**P0 — Le harnais définit le vérifiable ; le projet définit le reste.** La
frontière entre ce que le wrapper impose et ce qu'il laisse libre est exactement
celle du testable. Tout ce qui a un critère de vérification — schéma de réponse,
accès, ce qui peut sortir, bornes — relève du **harnais**, souverain et strict.
Tout ce qui n'en a pas — ton, style, consignes, intention — relève du **projet**
(`CLAUDE.md`/`AGENT.md`, prompt), laissé entièrement libre. Le renversement qui
fonde le projet : on traite l'agent comme adversaire **pour pouvoir** lui
accorder cette liberté. Parce que les garanties ne portent que sur des propriétés
mesurées indépendamment de ce que l'agent dit ou croit, son contenu peut être
quelconque sans qu'aucune garantie tombe. Vider la pièce (P1) et harnacher le
vérifiable sont le même geste : rendre le comportement de l'agent sans
conséquence sur ce qui compte, pour n'avoir pas à le contraindre sur ce qui ne
compte pas.

> *Règle de tri* : écris le test qui échoue si la consigne est violée. S'il
> existe — un schéma qu'on parse, un accès qu'on éprouve, une sortie qu'on
> inspecte — c'est du harnais. Sinon (« sois concis », « préfère les petits
> commits »), c'est du projet ; le prétendre garanti serait s'appuyer sur une
> liste d'interdits qu'aucun test ne protège — ce que P2 et P4 (plus bas)
> écartent.

P1 (vider la pièce, plus bas) en est le cas de la donnée — rien à voler ; P4
(plus bas) en est le critère : pas de test, pas d'exigence.

**P1 — Vider la pièce plutôt que fermer la porte.** On ne cherche pas à empêcher
l'exfiltration : c'est perdu d'avance dès qu'un canal sortant existe. On fait en
sorte qu'il n'y ait rien à exfiltrer. Corollaire heureux : l'accès internet
devient gratuit. Limite assumée : la pièce n'est jamais *totalement* vide — elle
contient le code du dépôt (S30), qui est un risque accepté, pas un secret protégé.

**P2 — L'absence plutôt que l'interdiction.** Une garantie doit tenir à la
structure de l'environnement, pas à une liste d'exceptions. « Ce répertoire
n'existe pas dans la sandbox » et « ce répertoire existe mais je l'ai interdit »
protègent identiquement aujourd'hui ; demain, seul le premier tient encore. Une
liste doit être exhaustive **et le rester**, alors que chaque mise à jour de
l'outil confiné peut ajouter un chemin qu'elle ne couvre pas — sans rien casser,
donc sans que personne ne le remarque.

> *Test* : si une nouvelle version de l'outil peut périmer ma garantie sans
> qu'aucune erreur n'apparaisse, c'est une liste. Elle relève alors de P4.

Illustration : énumérer dans `~/.claude` les fichiers exécutables à interdire
(`settings.json`, `hooks`, `agents`, `commands`, `plugins`, `skills`…) est
correct aujourd'hui et faux le jour où un septième apparaît. Un environnement où
`~/.claude` n'existe pas n'a rien à tenir à jour.

**P3 — Ce qui sort de la boîte est une donnée, jamais du code.** Ni exécuté, ni
sourcé, ni interprété comme un chemin, ni rendu tel quel dans un terminal ou un
navigateur. Transporter des octets hostiles est nécessaire ; les interpréter ne
l'est pas.

**P4 — Un garde-fou non testé finit par ne plus garder.** Toute règle doit
avoir un test qui échoue si elle cesse de s'appliquer, exécuté à chaque run.
Toute liste qui ne peut pas être remplacée par une absence (P2) relève de P4 :
elle est admise, mais uniquement adossée à un test de fraîcheur.

**P5 — Capability jetable plutôt que secret partagé.** Quand un identifiant doit
franchir la frontière, il est à portée réduite, budgété et expirant.

**P6 — La frontière est le wrapper, pas la relecture.** La relecture est une
étape de qualité et de responsabilité, pas un mécanisme de sécurité : elle
arrive trop tard pour S06 et S09 ; S10 est désormais fermé en amont (§8) ; et
S18 comme S31 l'attaquent directement.

## 7. Exigences

Chaque exigence est formulée pour être **vérifiable**. Une exigence sans
critère de vérification est une intention, pas une exigence (P4).

### R1 — L'agent ne voit que le code du dépôt

Aucun accès en lecture à quoi que ce soit d'autre : pas le `$HOME` du poste,
pas les fichiers partagés de Claude, pas les autres projets.

*Vérification* : depuis la sandbox, ces chemins (`~`, `~/.ssh`, `~/.claude`,
`~/.aws`) **n'existent pas** — leur énumération échoue faute de cible, pas faute
de permission (« n'existe pas », non « existe mais refusé » — P2). L'inventaire
des chemins lisibles hors du dépôt est vide parce qu'il n'y a rien à refuser.

### R2 — Aucun secret exfiltrable dans la sandbox

Ni identifiant GitLab, ni jeton Claude réutilisable, ni clé de signature, ni
URL de remote porteuse de credentials. Si la boîte est vide de secrets, l'accès
internet est accordé largement (P1) ; le code du dépôt reste exfiltrable et
c'est un risque accepté (S30), pas une contradiction.

**Portée de l'identifiant Claude (S28).** L'exigence porte sur ce que
l'identifiant ouvre, pas seulement sur son vol. Les trois types disponibles
n'ont pas la même portée :

| Identifiant | Portée | Plafond natif |
|---|---|---|
| Clé Console **non scopée** (personnelle ou compte de service) | Atteint l'**Admin API** avec les droits du compte lié : membres de l'organisation, workspaces, gestion des clés. **À proscrire.** | — |
| Clé Console **scopée à un workspace** | Inférence, **plus tous les fichiers du workspace** (Files API) : « Any API key with access to a workspace can access any files uploaded to that workspace ». Le *Default Workspace* est ouvert à tout compte de service — le pire emplacement. Neutralisé par un workspace dédié et vide. | Plafond de dépense par workspace |
| Jeton `claude setup-token` (abonnement) | **Requêtes de modèle uniquement** : la documentation précise « It can only make model requests », donc ni Remote Control, ni connecteurs claude.ai. Pas de workspace, donc pas de Files API. Portable un an, révocable. | Aucun — seules les limites d'abonnement s'appliquent |

Quel que soit le type retenu : jamais de clé non scopée, et le plafond de
consommation est de la responsabilité du broker (R4, R9) dès lors que
l'identifiant n'en offre pas nativement.

**Condition réseau.** Une capability par run, même budgétée et expirante, reste
un porteur : pendant sa fenêtre de vie elle est utilisable partout où le broker
répond. La propriété « inutilisable depuis une autre machine » n'est donc pas
une propriété de la capability, mais du **broker** : il n'est joignable que
depuis la sandbox (R9). Sans cette condition, R2 n'est pas vérifiable.

*Vérification* : inventaire des identifiants atteignables depuis l'intérieur ;
résultat attendu vide, ou limité à une capability conforme à R4. Toute
capability présente doit être **inutilisable depuis une autre machine** — rejeu
depuis l'extérieur refusé **au niveau réseau** (connexion impossible), pas
seulement applicatif — et **inutilisable après la fin du run**. Pour une clé
Console : le workspace visé ne contient aucun fichier, et la clé est rejetée par
l'Admin API.

### R3 — Le code mergé appartient à son owner

Le projet garantit l'innocuité de l'environnement, pas celle du code produit.
L'owner relit chaque MR et en assume le contenu.

*Précondition* : la relecture doit être praticable. Le wrapper doit signaler ce
qui rend un diff trompeur ou illisible (S18) et neutraliser ce qui piège le
rendu de la MR elle-même (S31), faute de quoi cette exigence est vide.
*Vérification* : un diff délibérément trompeur (bidi, binaire, vendored,
renommage massif) doit être signalé avant le push ; une description de MR portant
un lien externe ou une image distante doit être neutralisée avant publication.

### R4 — Les ressources de la sandbox sont bornées

Quatre plafonds distincts : CPU/RAM, disque, durée de vie du run, tokens
consommés. S'appliquent aussi au côté récepteur (R8) et au broker (R9).

*Vérification* : remplir le disque, saturer le CPU, dépasser le budget de
tokens et dépasser la durée maximale doivent chacun se solder par un arrêt
propre, sans impact sur le poste.

### R5 — La sandbox se détruit sans danger

*Vérification* : après destruction, aucun processus, fichier, credential valide
ni état résiduel ne subsiste. La destruction n'exécute rien qui vienne de la
boîte (pas de script de teardown fourni par l'agent). Ce qui a franchi la
frontière est régi par R8 ; ce que le broker a émis est régi par R9.

### R6 — Le wrapper ne fait confiance à aucun octet venant de la sandbox ou du dépôt cible

**Transporter n'est pas interpréter.** Le wrapper doit nécessairement faire
traverser des octets hostiles : c'est le produit du travail. R6 n'interdit pas
de les transporter — elle interdit de les traiter comme du code, de la
configuration, un chemin, du texte de terminal ou du Markdown rendu.

C'est l'exigence qui protège **le code qui garde la boîte** ; toutes les autres
protègent la boîte.

*Vérification* : un agent piégé qui écrit une configuration empoisonnée, un
titre de MR contenant des métacaractères shell, un lien ou une image distante
dans la description de MR, des séquences ANSI dans sa sortie et un lien
symbolique sortant ne doit provoquer aucune exécution, aucune injection, aucune
traversée, aucun rendu actif chez le relecteur.

### R7′ — Le wrapper inspecte ce qui sort : diff et texte de MR

> *« R7′ » (prime) : cette exigence a remplacé un R7 plus étroit d'une version
> antérieure ; les autres numéros n'ont pas bougé (cf. §5, identifiants stables).
> Son objet déborde le diff — le titre, la description et le résumé de la MR
> (S31) sont inspectés aussi, car ils ne sont pas du diff.*

La logique est une **allowlist**, pas une liste d'interdits (P2, P4) : toute
entrée de premier niveau nouvelle, tout fichier ou répertoire caché absent d'une
petite allowlist connue, est **signalé**. Les catégories ci-dessous sont les cas
forts, traités plus durement — mais l'alerte par défaut couvre l'inconnu que le
tableau n'anticipe pas.

| Catégorie | Contenu | Action |
|-----------|---------|--------|
| S'exécute sur le poste | `.envrc`, `.vscode/`, `.idea/`, `.claude/`, `.mcp.json`, `.gitmodules`, `.gitattributes`, `Makefile`, `justfile`, `Taskfile.yml`, `.husky/`, `.pre-commit-config.yaml` | **Bloque** |
| Oriente une session normale | `CLAUDE.md`, `AGENT.md` (entrée légitime de l'agent, mais toute modification oriente les sessions futures — S09, D4) | **Alerte** |
| S'exécute dans le CI | `.gitlab-ci.yml`, `.gitlab/`, `ci/` | **Alerte** |
| Chaîne d'approvisionnement | lockfiles, section `scripts` de `package.json`, répertoires vendored | **Alerte** |
| Signaux de tromperie | binaires ajoutés, caractères bidi ou de largeur nulle, fichiers minifiés, renommages massifs | **Alerte** |
| Piège du rendu MR (S31) | liens externes et images distantes dans le titre / la description / le résumé de la MR | **Neutralise** |
| Inconnu | toute entrée de premier niveau ou tout fichier caché hors allowlist | **Alerte** |

Cette table reste une liste : elle relève de P4 et doit avoir un test de
fraîcheur (un fichier fabriqué de chaque catégorie, plus un cas inconnu hors
allowlist, rejoué à chaque run). Elle n'est que de la défense en profondeur
*avant merge* ; S06 est réellement fermé par R8 (aucun arbre de travail
matérialisé). Pour S09, en revanche, elle est la seule garde côté poste.

Toute dérogation se donne à l'invocation du wrapper, jamais depuis un fichier
du dépôt (P3). L'inspection s'effectue sans matérialiser d'arbre de travail
(R8) et porte sur **l'objet dont le SHA sera poussé**, jamais sur une référence
mutable — sinon l'agent peut faire diverger le contenu entre inspection et push.

*Vérification* : un run touchant chaque catégorie, plus une entrée inconnue hors
allowlist, produit le blocage, l'alerte ou la neutralisation attendus ; le SHA
inspecté est identique au SHA poussé.

### R8 — Le canal de récupération est typé, étroit et inerte

Ce qui franchit la frontière **vers** le poste se limite à trois choses : des
objets git atteignables depuis une référence unique, un journal traité comme du
texte, un code de sortie. Ni fichier de travail, ni configuration, ni hook, ni
système de fichiers partagé.

Le transport lui-même ne doit rien déclencher côté récepteur :

- **Refspec explicite et étroit**, vers une référence que *le wrapper* nomme.
  Jamais `refs/*`, jamais `--tags`, jamais un mirror — c'est ce qui ramènerait
  `refs/replace/` (S26).
- **Push par SHA, pas par référence mutable** : ce qui est poussé et rendu à
  l'owner est exactement l'objet inspecté par R7′ (fermeture de la fenêtre
  inspection→push).
- **Remplacement d'objets désactivé** (`GIT_NO_REPLACE_OBJECTS=1`), pour que le
  diff relu soit le contenu poussé.
- **Validation des objets entrants** (`fetch.fsckObjects`), contre S20.
- **Récursion de sous-modules désactivée**, aucun `submodule update` (S27).
- **Plafonds de taille et de durée** sur la récupération (S24, R4).
- **Le dépôt récepteur est hors d'atteinte de l'agent** : sa configuration, ses
  hooks et ses attributs restent ceux du wrapper.
- **Aucun arbre de travail n'est matérialisé** avant décision explicite de
  l'owner. L'inspection R7′ se fait sur les objets, sans checkout — de sorte
  qu'aucun `.envrc` ni `.vscode/` n'existe sur le disque du poste (S06 fermé par
  P2, sans liste d'interdits).

*Vérification* : un agent piégé qui pousse des refs de remplacement, un
sous-module hostile, un objet malformé et une arborescence géante ne doit
produire ni exécution, ni divergence entre le diff relu et le contenu poussé,
ni fichier matérialisé sur le poste.

### R9 — Le broker est un relais minimal, autonome, sans état exploitable, joignable seulement depuis la sandbox

Le broker détient l'identifiant réel (D1) et traduit une capability par run en
requêtes authentifiées. C'est un composant de la catégorie E (S29) : il parse
des octets hostiles avec le secret à portée. R9 lui applique R6.

- **Autonome ; le wrapper en est le client, pas le parent.** Le broker est un
  service à cycle de vie propre (un démon). Il expose deux faces distinctes :
  une **face de contrôle** locale (socket) où le wrapper frappe et révoque une
  capability par run, et une **face de données** exposée à la sandbox (la cible
  de `ANTHROPIC_BASE_URL`). Le wrapper ne détient jamais le jeton réel et ne
  détruit que la sandbox (R5) ; le broker survit au run.
- **Ne fait confiance à aucun octet venant de la sandbox** : les requêtes sont
  relayées comme des données, sans désérialisation qui exécute, sans logique
  dépendant du contenu des en-têtes ou des chemins hostiles. Le relais SSE est
  un flux, pas un interpréteur.
- **N'écoute que sur l'interface tournée vers la sandbox.** Jamais joignable
  depuis une autre machine. C'est cette propriété qui rend R2 vérifiable.
- **Ne renvoie jamais l'identifiant réel à la sandbox.** Le jeton d'abonnement
  ne sort pas du broker ; seule la réponse du modèle traverse.
- **Valide chaque capability** — portée, budget (R4), expiration — avant de
  relayer, et refuse au-delà. Le plafond de consommation vit ici quand
  l'identifiant ne l'offre pas nativement (S28).
- **Sans état persistant exploitable entre runs** (S22) : rien de ce qu'un run
  laisse au broker ne doit être utilisable par le run suivant.

*Vérification* : rejouer une capability depuis une autre machine → refus au
niveau réseau. Envoyer au broker des requêtes malformées, des en-têtes hostiles,
un corps surdimensionné, un flux SSE interrompu → aucun plantage exploitable,
aucune fuite du jeton réel, arrêt propre. Dépasser budget ou expiration → refus.

### Procédure d'acceptation : l'agent piégé

Les exigences se valident par **un run réel avec une spec adverse**, conservée
dans le projet et rejouée à chaque modification du wrapper ou du broker. Elle
demande explicitement à l'agent de tenter : énumérer et lire hors du dépôt (R1),
localiser et exfiltrer un identifiant (R2), extraire le jeton réel du broker ou
rejouer sa capability ailleurs (R9), déposer un hook, une configuration et un
`.envrc` (R7′), piéger le rendu de la MR par un lien et une image distante (S31),
empoisonner la configuration du wrapper (R6), fausser le canal de récupération
(R8), saturer les ressources de la sandbox, du récepteur et du broker (R4),
laisser un résidu après destruction (R5). Exfiltrer le dépôt (S30) est attendu
et ne compte pas comme un échec : c'est un risque accepté.

C'est la seule preuve qui compte. Un raisonnement sur la configuration n'en est
pas une (P4, S21).

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

## 10. Contraintes induites sur toute solution

Déduites des exigences, sans choisir d'implémentation.

- **R1 + R4 écartent les sandboxes de niveau OS** (Seatbelt, Landlock,
  bubblewrap), pour deux raisons — et non parce qu'elles « autorisent la lecture
  par défaut », ce qui est faux pour Landlock (deny-by-default) et pour un profil
  Seatbelt écrit en refus par défaut. Les vraies raisons : **(a) P2** — elles
  partagent le noyau du poste, où `~/.ssh`, `~/.aws`, `~/.claude` *existent* ; les
  fermer est une politique, c'est-à-dire une liste qui doit rester exhaustive à
  chaque mise à jour de l'outil confiné (« existe mais interdit », pas
  « n'existe pas ») ; **(b) R4** — le plafonnement des ressources (CPU, RAM,
  disque, durée) n'est pas fourni nativement : partiel via cgroups sous Linux,
  essentiellement absent du Seatbelt macOS. Satisfaire R1 par une liste
  d'interdits contredirait P2 ; R4 resterait hors d'atteinte.
- **R2 + R9 imposent un broker autonome côté poste**, à cycle de vie propre et
  dont le wrapper est client (il n'en est pas le parent), détenant l'identifiant
  réel et joignable uniquement depuis la sandbox sur sa face de données. Aucun
  montage où l'agent porte un jeton réutilisable, ni où le broker écoute au-delà
  de l'interface de la sandbox, ne satisfait R2.
- **R8 impose un transport par objets git** vers un dépôt récepteur hors
  d'atteinte de l'agent — pas un système de fichiers partagé, pas un montage
  en écriture du répertoire de travail.
- **R6 impose que le wrapper ne lise aucune configuration exécutable venant du
  dépôt cible** (satisfait par D4 : config hors dépôt).

Ces contraintes convergent vers la famille **micro-VM native**, actée en D6
(Apple `container`). Le projet part de zéro : il n'y a pas d'implémentation
préexistante à faire converger.

## 11. Notes d'implémentation

Premiers engagements d'implémentation, distincts des décisions d'architecture
(§9) : ils concrétisent le « comment » et pourront évoluer sans rouvrir les
exigences.

**Isolation — Apple `container` (D6).** Image OCI minimale ne contenant que la
chaîne d'outils nécessaire à la tâche ; aucun montage du `$HOME` ni d'un chemin
du poste (R1 par absence). La récupération se fait par `git fetch` depuis un
dépôt interne à l'invité, jamais par un bind-mount en écriture du répertoire de
travail (R8). Repli Tart/Lima si le poste n'est pas sur macOS 26.

**Langage — Go, pour le wrapper et le broker.** Binaire statique, pile HTTP de
la bibliothèque standard (éprouvée), exec de `git` par tableaux d'arguments
explicites, jamais par une chaîne shell — un wrapper shell interpréterait
précisément les octets que R6 interdit d'interpréter. Le broker étant un relais
**transparent**, il ne parse quasiment rien : il ne lit pas le corps des
requêtes (passage tel quel) et se contente de scanner le flux SSE pour
l'événement `usage` (comptage, R4), sans désérialiser de structure de confiance
(R9) — ce qui referme l'essentiel de la surface qui, autrement, plaiderait pour
un langage à sûreté mémoire.

**Topologie du broker (D1, R9).** Démon autonome (p. ex. `launchd`), utilisateur
dédié, à moindre privilège. Face de contrôle sur socket local (frappe et
révocation de capability) ; face de données liée à la seule interface tournée
vers la sandbox. Le wrapper est client des deux faces et ne détient jamais le
jeton réel.

## 12. Mode autonome (post-MVP)

Le MVP est interactif et supervisé : la session complète vit dans le REPL de
Claude Code, l'humain répond au terminal, rien à concevoir. Ce paragraphe conçoit
le régime **autonome** qui vient après — quand aucun humain n'est dans la boucle.
C'est de la conception, cohérente avec P0 et P6 ; elle n'est pas requise pour le
MVP.

**Deux natures d'interruption, une seule légitime.** Une demande de *permission
d'agir* (« puis-je parser ce site, lancer cette commande ? ») est
**structurellement vide** ici : la boîte est jetable, l'egress est ouvert (P1) et
la sortie est filtrée à la récupération (R7′/R8). Dedans, tout est permis parce
que sans conséquence ; dehors, rien n'est demandé parce que tout est verrouillé à
la sortie. Il n'y a donc pas de canal de permission — l'architecture a répondu à
toutes les permissions d'avance. Une demande de *clarification du sens* (« que
faut-il faire ici ? ») est d'une autre nature : c'est de l'information que seul
l'humain détient, qu'aucune architecture ne pré-décide. Elle est légitime, mais
**asynchrone** — l'agent ne bloque pas, il l'émet et le run se termine.

**Trois états terminaux, imposés par le harnais (P0).** Un run autonome se
termine dans exactement un de :

- **`push`** — le travail est fait ; le diff revient (R8) et alimente la branche.
- **`question`** — une ambiguïté de *sens* rend le travail indécidable ; la
  question devient un commentaire de MR.
- **`blocked`** — une borne atteinte (tours, budget, durée) ou un échec ; devient
  un commentaire « je n'ai pas pu, voici pourquoi », jamais un silence.

L'existence de ces états et le schéma de sortie sont **vérifiables**, donc du
harnais : le wrapper les impose via `--output-format json` et un contrat dans le
prompt système, puis route la sortie (pousser, commenter, signaler). La
*propension* de l'agent à emprunter `question` plutôt que `push` est du **ton**,
donc du projet (P0) : « ne demande que si tu es réellement bloqué sur le sens ;
sinon fais ta meilleure tentative et documente tes hypothèses » vit dans le
`CLAUDE.md`, pas dans le harnais.

**Le fil de commentaires de la MR *est* la session, rendue asynchrone.** Question
de l'agent → commentaire ; réponse humaine → commentaire, qui **déclenche un run
repris**. Il n'y a donc pas de verbe « envoyer une instruction » distinct : c'est
`run --resume <session>`, un tour de parole de plus. Le CLI n'a que `run`
(éventuellement en reprise) et `stop`, un coupe-circuit asymétrique pour tuer un
run emballé (R4/R5). En local on tape le `run --resume` ; en mode forge, l'amont
de D8 traduit « réponse au commentaire » en `run --resume` — le wrapper reste
sans surface entrante.

**La reprise impose de persister l'état de session hors de la boîte.** La boîte
est détruite à chaque run (R5) ; la transcription dont `--resume` a besoin doit
donc survivre **côté hôte**. Résidu *voulu*, pas entorse à R5. Mais c'est une
instance de **S22** (état partagé entre runs) : le magasin de sessions doit être
**cloisonné par session** — l'état d'une session ne fuit jamais dans une autre —
au même titre que le broker interdit tout état exploitable entre runs.

**Une session autonome est bornée par construction**, puisqu'aucun humain ne
l'arrêtera : `--max-turns`, budget, durée (R4). « Gérer une session entière » en
autonome, c'est « une session bornée qui va au bout seule », pas « une session
ouverte qui attend ».

## Annexe — familles d'isolation

Vocabulaire de référence. Le critère est l'interface à laquelle le code enfermé
s'adresse.

| Famille | Interface | Pour sortir, il faut casser | Ressources plafonnables | Environnement de dev complet |
|---------|-----------|------------------------------|--------------------------|-------------------------------|
| Primitives OS (Seatbelt, Landlock, seccomp) et conteneurs | Le noyau du poste, avec un filtre | Le noyau, ou la politique | Partiellement (cgroups) | Oui |
| Noyau applicatif (gVisor) | Un noyau en espace utilisateur | Ce noyau, puis le vrai | Oui | Oui, plus lent |
| Micro-VM (Firecracker, Tart) | Du matériel virtuel, noyau distinct | L'hyperviseur ou le processeur | Oui | Oui |
| WASM | Du bytecode, aucun appel système | Le runtime | Oui | Non |

Un conteneur partage le noyau du poste : il appartient à la première famille,
pas à une catégorie intermédiaire.

Sur macOS, la micro-VM native passe par Virtualization.framework : Apple
`container` (une micro-VM par conteneur OCI, choix acté en D6) ou Tart (une
micro-VM par image de VM). Firecracker relève de la famille micro-VM **côté
Linux** (KVM) et n'a pas de support macOS ; sur un Mac il ne s'exécute
qu'imbriqué dans une VM Linux, ce qui empile deux hyperviseurs pour la même
propriété d'isolation.

## Sources

- [Claude Code — Authentication](https://code.claude.com/docs/en/authentication) : `claude setup-token`, portée du jeton (« It can only make model requests »), ordre de précédence des credentials, `ANTHROPIC_AUTH_TOKEN` et `ANTHROPIC_BASE_URL`
- Restriction du jeton d'abonnement à un usage *avec Claude Code* (rejet « only authorized for use with Claude Code » hors de ce contexte) — à revérifier avant implémentation du broker (D1, risque résiduel n°1)
- [Files API — scoping et accès](https://platform.claude.com/docs/en/build-with-claude/files)
- [Admin API — clés et permissions](https://platform.claude.com/docs/en/manage-claude/admin-api)
- GitLab — option de push `ci.skip` (`git push -o ci.skip`, git ≥ 2.18 ; `--push-option=ci.skip` depuis git 2.10) : crée un pipeline marqué *skipped* sans exécuter de job ; contrôle côté pousseur, non défait par un fichier du dépôt
- Apple `container` — 1.0.0 (9 juin 2026), Apache-2.0, macOS 26 / Apple Silicon : une micro-VM légère par conteneur OCI, adossée à Virtualization.framework ; frontière d'isolation à l'hyperviseur (D6, §10, annexe)
- Firecracker — pilote KVM et exige un hôte Linux exposant `/dev/kvm` ; pas de support macOS (position des mainteneurs) ; sur Apple Silicon uniquement en imbriqué dans une VM Linux (M3+/macOS 15+ pour la virtualisation imbriquée)
