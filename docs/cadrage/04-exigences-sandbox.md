# 7. Exigences — la boîte (R1 à R5)

Chaque exigence est formulée pour être **vérifiable**. Une exigence sans
critère de vérification est une intention, pas une exigence (P4).

## R1 — L'agent ne voit que le code du dépôt

Aucun accès en lecture à quoi que ce soit d'autre : pas le `$HOME` du poste,
pas les fichiers partagés de Claude, pas les autres projets.

*Vérification* : depuis la sandbox, ces chemins (`~`, `~/.ssh`, `~/.claude`,
`~/.aws`) **n'existent pas** — leur énumération échoue faute de cible, pas faute
de permission (« n'existe pas », non « existe mais refusé » — P2). L'inventaire
des chemins lisibles hors du dépôt est vide parce qu'il n'y a rien à refuser.

## R2 — Aucun secret exfiltrable dans la sandbox

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

## R3 — Le code mergé appartient à son owner

Le projet garantit l'innocuité de l'environnement, pas celle du code produit.
L'owner relit chaque MR et en assume le contenu.

*Précondition* : la relecture doit être praticable. Le wrapper doit signaler ce
qui rend un diff trompeur ou illisible (S18) et neutraliser ce qui piège le
rendu de la MR elle-même (S31), faute de quoi cette exigence est vide.
*Vérification* : un diff délibérément trompeur (bidi, binaire, vendored,
renommage massif) doit être signalé avant le push ; une description de MR portant
un lien externe ou une image distante doit être neutralisée avant publication.

## R4 — Les ressources de la sandbox sont bornées

Quatre plafonds distincts : CPU/RAM, disque, durée de vie du run, tokens
consommés. S'appliquent aussi au côté récepteur (R8) et au broker (R9).

*Vérification* : remplir le disque, saturer le CPU, dépasser le budget de
tokens et dépasser la durée maximale doivent chacun se solder par un arrêt
propre, sans impact sur le poste.

## R5 — La sandbox se détruit sans danger

*Vérification* : après destruction, aucun processus, fichier, credential valide
ni état résiduel ne subsiste. La destruction n'exécute rien qui vienne de la
boîte (pas de script de teardown fourni par l'agent). Ce qui a franchi la
frontière est régi par R8 ; ce que le broker a émis est régi par R9.


*Suite : R6 à R9 et la procédure d'acceptation dans [05-exigences-frontiere.md](05-exigences-frontiere.md).*
