# Problème, composants, périmètre et adversaire

*Cadrage §1–§4 — index : [README.md](README.md).*

## 1. Le problème

On veut faire travailler Claude Code sur un dépôt, **sans validation manuelle
pendant l'exécution** (`--dangerously-skip-permissions`), et récupérer le
résultat sous forme de Merge Request à relire.

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
