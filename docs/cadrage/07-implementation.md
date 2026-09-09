# Contraintes, notes d'implémentation et familles d'isolation

*Cadrage §10–§11 et annexe — index : [README.md](README.md).*

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
du poste (R1 par absence). L'image de base est définie dans `images/sandbox/`
(D9) : Debian trixie slim épinglée par digest, binaire natif de Claude Code
épinglé par version et SHA256, `git` et les outils courants du modèle
(curl, jq, patch, ps, python3), utilisateur `agent` (uid 1000) avec pour seul
contenu de `/home/agent` l'état de premier lancement de Claude Code (onboarding
fait, `/work` de confiance) et le dépôt dans `/work`, `DISABLE_UPDATES=1` pour
que la version épinglée soit celle qui s'exécute. Ses garanties sont structurelles
(P2) : le Dockerfile est la spec et le build le test ; ce qui peut les défaire
est l'appel du wrapper (montages, variables, utilisateur), testé en Go avec
lui. Les chaînes d'outils propres à un projet viennent en couche au-dessus de
cette base. 

**Pilotage (D10)** : le CLI `container` (1.3.1) exécuté par
tableau d'arguments, jamais le framework Swift ; cove ne parse que le stdout
JSON de `list` et `inspect`. Cove est une interface entre le moteur de VM
(`container` aujourd'hui, Firecracker demain) et le harnais : un CLI sans
démon, à la manière de terraform. Une VM survit à la fin de cove comme à celle
du CLI `container` (service launchd propre, signaux au CLI non transmis) ; sa
destruction est un verbe explicite, jamais un effet de bord. Trois rôles (§12) :
`run` crée la sandbox et rend la main dès que la VM tourne, `send` parle à
l'agent (un processus Claude par tour, lancé par `exec`, repris par son
identifiant de session), `stop` arrête la VM.

*Processus 1 de la VM.* Comme aucun Claude n'occupe la place entre deux tours,
la VM a besoin d'un processus 1 qui attend de `run` à `stop`, et Linux traite ce
processus à part : un signal à comportement par défaut ne lui est pas délivré,
et les orphelins des autres processus lui sont rattachés. Mesuré sur 1.3.1 avec
`sleep` seul en processus 1 : `stop` envoie SIGTERM dans le vide et tue après
son délai (5 s, code 137), et un processus laissé par un `exec` reste zombie
(`ps` : état `Z`). Avec `--init`, `container` place devant la commande son
propre init (`/.cz-init`, la commande `init` de vminitd du projet Containerization,
sous l'utilisateur de l'image) : il bloque tous les signaux et les relaie à son
enfant par `sigtimedwait`, moissonne par `waitpid`, et sort avec le code de
l'enfant. Mesuré : `stop` sans délai, aucun zombie, code de sortie rendu
(`sh -c 'exit 7'` : 7). L'enfant est `sleep infinity`, bouche-trou : GNU `sleep`
accepte `infinity` parce qu'il lit son argument par `strtod`, ce que la base
Debian (D9) garantit et que BusyBox ou BSD ne garantissent pas. vminitd a aussi
une commande `pause` (processus 1 qui attend et moissonne, comme celui de
Kubernetes) mais le CLI ne l'expose pas. Ce bouche-trou est destiné à être
remplacé par un processus cove propre à la VM, au même endroit de l'argv : c'est
là que vivent les actions qui exigent un processus résident, la durée maximale
de la VM (R4, aucun drapeau côté `container`), les règles netfilter de S32
posées en root avant de descendre en uid 1000, le test de fraîcheur de P4, et la
recopie du stdio de Claude vers `logs`, qui ne voit que le processus 1. Cove
reste ainsi sans démon : le résident vit dans la boîte, tout ce qu'il renvoie
est hostile (R6), il ne détient aucun secret (R2).

*Drapeaux.* Ceux de `docker run` que le rôle de `run` justifie (`--name`,
`--rm`, `--keep`, `--cpus`, `-m`, `-e`), passés tels quels, et rien d'autre :
cove construit le tableau d'arguments lui-même, donc ce qui déferait R1, R2 ou
D9 (`-v`, `--mount`, `-u`, `-w`, `--ssh`, `--env-file`, réseau, capabilities,
`--label`) et ce que le rôle exclut (`-i`, `-t`, une commande) n'existent pas
plutôt que d'être interdits (P2) ; ce tableau est testé en Go (`internal/sandbox`).
`-d` est posé par cove. Observé : `container run -d` écrit le nom de la VM sur
stdout (un UUID sans `--name`) et sa progression sur stderr ; `--memory 512m` en
minuscule est accepté ; `-e` est un passe-plat total pour l'instant. Chaque VM
porte le label `cove=sandbox`, par lequel cove reconnaît les siennes.

*Arrêt.* `stop` prend un ou plusieurs noms ou identifiants, que `container`
résout lui-même (observé sur 1.3.1 : l'identifiant est le nom, correspondance
exacte, aucun préfixe ; un nom inconnu est signalé, les autres cibles sont
traitées et le code est 1), ou `--all`. Les drapeaux `-s` et `-t` de
`docker stop`, `podman stop` et `container stop` passent tels quels ; le délai
par défaut reste celui de `container` (5 s, contre 10 chez docker et podman).
Règle d'or : cove n'arrête jamais une VM qu'il n'a pas lancée. Le magasin de
`container` est partagé (la VM `buildkit` d'Apple y vit), donc chaque cible est
confrontée au label dans `list --all --format json` avant tout appel, et
`--all` se résout en la liste des sandboxes de cove en marche, jamais en
`container stop --all`. La garantie tient par construction ; ses deux résidus
exigent un acteur côté hôte, hors modèle de menace : le label est posable par
quiconque sur le poste, et une VM peut changer entre la lecture de la liste et
l'arrêt. Codes : celui de `container`, 1 si une cible a été refusée ou est
inconnue, 2 en erreur d'usage, 125 quand cove n'a pas pu exécuter `container`
(comme `run` ; docker réserve 125 à `run`, podman le généralise, `container`
ne rend que 1).

*Interaction.* `send` est une couche mince sur `container exec` : deux régimes
dans un verbe, comme `sbx run` chez Docker, seul harnais du marché à les porter
ensemble. Sans prompt, `exec -it` attache le REPL de l'agent au terminal ; avec
un prompt, `exec` lance `claude --print --output-format json` et cove recopie
le JSON sur stdout sans le parser ni promettre son schéma (l'inverse de `list`,
qui rend des objets de cove : unifier la sortie de tous les agents serait un
contrat intenable). Pas de mode texte : ce serait le seul chemin à exiger un
filtre. Cove possède l'argv de l'agent : le prompt positionnel, `--session-id`,
`-n`, `--resume`, `--continue`, et rien d'autre ; pas de `--` qui passerait l'argv tel quel
comme le fait `sbx`, parce que le contrat de sortie tient à des drapeaux que
l'utilisateur écraserait (P2). Identité du fil : cove tire un UUID v4 et le
pose en `--session-id`, donc aucun octet lu dans la boîte ne devient un
identifiant (R6) ; `-n` nomme le fil et `--resume` accepte l'UUID comme le nom,
`claude` résolvant les deux, cove ne tient aucun index. Chaque `send` sans
`--resume` ouvre un fil neuf : deux tours de suite ne se parlent pas sauf à le
dire, seule lecture sans implicite dans une sandbox qui porte plusieurs fils.
Ceux-ci partagent `/work` et cove n'arbitre ni leurs écritures ni leurs noms en
double ni deux tours à la fois dans le même fil : le contrôle appartient à qui
pilote (D8), et le diff qui sort d'une sandbox à plusieurs fils ne se rattache à
aucun d'eux. L'identifiant n'est écrit nulle part en régime piloté, où stdout
doit porter le JSON et rien d'autre et où il est déjà le `session_id` ; en
régime attaché, sans JSON et stdout étant le PTY, il est annoncé sur stderr.
Neutralisation : aucune. Le JSON échappe les caractères de contrôle `U+0000` à
`U+001F` (RFC 8259), donc l'octet ESC n'est pas dans le flux, absence plutôt que
règle à tenir à jour ; ce qui le décode ensuite (`jq -r`) le rematérialise et en
répond ; le PTY passe brut, exception à R6 déjà nommée au §12, qui devient
permanente puisque le régime attaché reste. Ce n'est pas une position générale
sur le filtrage : le même texte rendu dans une MR relève de R7′ et de S31. La
cible est confrontée au label comme pour `stop` ; une sandbox arrêtée n'est pas
démarrée pour l'occasion, `container exec` la refuse (`is not running`, 1) et
cove rend ce refus tel quel. Les bornes (TTL, tours, budget) sont hors de ce
verbe (R4, D5). Mesuré sur `claude` 2.1.258 (hôte) puis 2.1.236 (image) : un
`--session-id` imposé revient tel quel dans le `session_id` ; `--resume`
reprend par UUID et par nom, en print mode aussi, historique porté ; `-n`
persiste dans la transcription (enregistrement `custom-title`), pas seulement
dans le registre des sessions vivantes ; deux fils de même nom font échouer
`--resume <nom>` en listant les UUID candidats au lieu d'en choisir un ; un
tour qui échoue laisse stdout vide ou strictement JSON, écrit du texte sur
stderr et sort 1 ; `--continue` ignore les sessions créées en print mode et
repart à neuf sans le dire, donc cove ne le porte qu'en régime attaché (`-c`,
seule reprise dont l'identité n'est pas la sienne) et le refuse avec un prompt
en erreur d'usage. Codes : celui de `container exec`, qui
porte celui de `claude`, 1 si la cible est refusée, 2 en erreur d'usage, 125
quand cove n'a pas pu exécuter `container`.

**Réseau (D10)** :
la VM reçoit une adresse non stable du réseau NAT `default`
(`192.168.64.0/24`), lue après démarrage ; l'hôte n'est joignable qu'à
l'adresse de passerelle `192.168.64.1` sur `bridge100`, interface partagée par
les VMs qui n'existe que pendant qu'une VM tourne ; l'hôte ne joint jamais la
VM ; le LAN, internet et les autres VMs du même réseau sont joignables (S32),
un réseau NAT par run isole les runs entre eux, le mode `--internal` coupe
tout, hôte compris, et le LAN se ferme depuis l'invité par nftables, posé en
root par `exec` avant l'agent avec `--cap-add NET_ADMIN` au lancement, internet
restant ouvert (S32). Pour l'instant la VM parle directement à l'API Anthropic ;
le broker (D1) est hors périmètre. La
récupération se fait par `git fetch` depuis un dépôt interne à l'invité, jamais
par un bind-mount en écriture du répertoire de travail (R8).
**Plafonds (R4, D5)** : CPU et mémoire par `--cpus` et `--memory` (la mémoire
dépassée vaut un OOM kill, exit 137) ; disque et durée n'ont aucun drapeau
dans `container`. Repli Tart/Lima si le poste n'est pas sur macOS 26.

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
