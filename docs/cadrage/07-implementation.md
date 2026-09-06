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
