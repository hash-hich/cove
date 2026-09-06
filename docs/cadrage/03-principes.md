# 6. Principes directeurs

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
