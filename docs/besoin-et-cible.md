# Cove : compte rendu du besoin et de la cible

Version française de [need-and-target.md](need-and-target.md).

L'ordre du raisonnement est volontaire : formuler le besoin, puis décrire la
cible. Le relevé des solutions ([sandboxes-ecosystem.md](sandboxes-ecosystem.md))
vient après, pour vérifier que les principes de la section 1 ne sont pas des
inventions locales et pour en rapporter les idées qui valent d'être reprises.

## 1. Le besoin

### 1.1 Deux usages, un seul outil

**Usage A, le poste du développeur.** Un dev lance un agent de code sur un
dépôt, sur son Mac, et le laisse travailler sans valider chaque commande.

**Usage B, le traitement sans supervision.** Une Merge Request s'ouvre sur
GitLab, un agent la prend en charge de bout en bout, sans humain devant.
Plusieurs MR se traitent en parallèle. La partie GitLab (webhook, jetons,
commentaires) est hors de ce document.

Les deux usages doivent partager le même environnement, la même politique et
le même budget. Ce que le dev lance chez lui est exactement ce que le runner
lancera sur la MR. Sans cette continuité, deux outils valent mieux qu'un.

### 1.2 Pourquoi une sandbox d'agent

Aujourd'hui, la seule chose qui borne un agent est l'humain qui le regarde.
Cet humain ne passe pas à l'échelle et, passé quelques heures, ne lit plus.
Et il ne sait pas non plus ce qui tourne : la moitié de ce qui détermine le
comportement de l'agent n'a jamais été choisie pour ce run. Elle vient du
poste, s'est accumulée, et ne se voit nulle part.

La sandbox fait deux choses. Elle permet de passer d'un modèle de
**surveillance** à un modèle d'**infrastructure** : la frontière est posée une
fois, la relecture se déplace de chaque action vers le résultat. Et elle
**remplace ce qui est hérité par ce qui est déclaré** : ce qui entre dans le
run est nommé, le reste n'entre pas.

Tout le reste en découle.

### 1.3 Deux conceptions de la sandbox

Le mot recouvre deux idées qui ne mènent pas au même produit.

L'**enceinte** est un lieu protégé où l'on travaille : elle persiste, on y
revient, le code du poste y est monté, la frontière se négocie en cours de
route, et l'on y entre à la main puisque c'est le but. Elle est optimisée pour
le confort et le retour immédiat.

L'**environnement jetable** est matérialisé pour une tâche à partir d'une
déclaration : il existe le temps du run, le code y est copié, la frontière est
fixée avant et ne bouge plus, personne n'y entre à la main. Il est optimisé
pour la reproductibilité et le parallélisme.

| | Enceinte | Environnement jetable |
|---|---|---|
| Ce que c'est | Un lieu où je travaille | Un environnement pour une tâche |
| Durée | Persiste, on y revient | Le temps du run |
| Le code | Monté depuis le poste | Copié dedans |
| La frontière | Se négocie en route, un clic pour débloquer | Se déclare avant, ne bouge plus |
| On y entre | Oui, c'est le but | Non, jamais à la main |
| Optimisé pour | Le confort, le retour immédiat | La reproductibilité, le parallélisme |

La quasi-totalité des outils existants construisent une enceinte, et cela
explique le paysage : trois choses qu'on y trouve rarement ne sont pas des
oublis, **elles n'ont pas de sens dans la première conception**. Un plafond par
run, quand la sandbox n'est pas un run mais un lieu. Un contexte déclaré, quand
on veut justement y retrouver ses affaires. Zéro reliquat, quand la persistance
est le service rendu.

Le précédent est Docker. Ses premières années se sont passées à l'utiliser
comme une VM légère dans laquelle on entre en ssh, où l'on installe des
choses, que l'on redémarre. Il a fallu des années pour arriver à l'image comme
déclaration, au conteneur jetable, à l'état sorti dehors. Le `-v $(pwd):/app`
avec un shell dedans, c'était le confort qui dissolvait la frontière : c'est
exactement le montage du répertoire courant que font les enveloppes
d'aujourd'hui.

Cove prend la seconde conception. Ce n'est pas une enceinte meilleure, c'est
l'autre prémisse.

**Le pari.** L'enceinte n'est pas une erreur : elle gagne sur le confort, et
c'est ce que veut un dev à sa machine. Notre pari est que la conception
jetable sert quand même l'usage A, comme l'image Docker a fini par servir le
développement local, parce que la contrepartie de la contrainte est que ce qui
tourne chez le dev est ce qui tournera sur la MR. Si ce pari est faux, les deux
usages divergent et il faut deux outils.

### 1.4 Les objectifs, en cinq familles

**A. Laisser l'agent travailler.** Sans frontière, l'agent est bridé bien
avant les questions de sécurité : il demande la permission, n'ose rien
détruire, marche sur les autres runs, sature le poste, et ne sort pas de la
machine où il a été lancé.

1. **Zéro fatigue de permission.** Les prompts de permission finissent
   toujours acceptés sans lecture. L'agent doit tourner sans en poser aucun.
2. **Destructif légitime.** Recréer la base de test, installer un paquet,
   changer la configuration globale, lancer Docker ou Kubernetes. Sans
   frontière, l'agent est condamné à une posture de lecture.
3. **Non-interférence.** Le besoin derrière le parallélisme n'est pas N runs
   à la fois, c'est N runs sans collision : ports, caches, état git, démon
   Docker, jeux de données partagés.
4. **Disponibilité du poste.** Des plafonds processeur, mémoire et disque
   pour que la machine reste utilisable pendant que l'agent travaille.
5. **Portabilité du run.** Le même travail sur le portable, en intégration
   continue, sur une machine cloud. Sans environnement défini, « le passer
   en CI » est une réécriture.

**B. Borner.** Ce qui rend l'autonomie acceptable.

6. **Dégâts contenus.** Une mauvaise commande ne doit effacer ni une base de
   données, ni un répertoire personnel.
7. **Aucun effet hors de la VM.** Un agent détourné, par le contenu d'une MR
   ou par un bug, ne doit pas pouvoir toucher l'hôte ni contourner ses
   restrictions en écrivant des scripts.
8. **Pas d'autorité ambiante, et rien de monnayable dedans.** Sur un poste,
   l'agent hérite de l'identité de l'humain : clés SSH, kubeconfig de
   production, cookies, jetons de tout. Le besoin n'est pas seulement
   d'empêcher la fuite d'un secret, c'est que l'agent n'en détienne aucun.
   Ce qu'il porte doit être sans valeur pour qui le vole : une habilitation
   qui ne vaut que depuis la VM où elle a été posée, et que le temps du run.
9. **Arrêt dur.** Une boucle doit pouvoir être coupée avec certitude, sur le
   budget ou sur la durée. Détruire la VM est le seul interrupteur qui
   marche ; tuer un arbre de processus, non.

**C. Déclarer le contexte.** Le comportement d'un agent est déterminé par son
contexte, et ce contexte est bien plus large que le prompt.

10. **Tout ce qui entre est nommé.** Image épinglée par empreinte, version de
    l'agent, identifiant exact du modèle, fichiers d'instructions, serveurs
    d'outils, crochets et compétences, variables d'environnement, commit du
    dépôt, domaines joignables, prompt. Rien d'autre.
11. **Zéro reliquat.** Aucune mémoire persistante, aucune session reprise,
    aucun cache ni fichier d'un run précédent, aucune configuration du poste.
    Ce qui n'est pas déclaré est absent.

Ce que ces deux règles éliminent, et qui agit aujourd'hui sans qu'on le voie :

| Entrée du contexte | Ce qui l'injecte à notre insu |
|---|---|
| Fichiers d'instructions | Le fichier global du poste, en plus de ceux du dépôt |
| Mémoire persistante | Une note écrite lors d'un run précédent, sur une hypothèse devenue fausse |
| Outils : MCP, crochets, compétences, greffons | La configuration du poste, différente d'un dev à l'autre |
| Version de l'agent | La mise à jour automatique, entre deux runs |
| Modèle | Un alias qui bouge, une bascule côté fournisseur |
| Chaîne d'outils et variables | Ce qui traîne sur le poste |
| État du disque | Les restes d'un run précédent, un cache, un fichier non suivi |
| Ce que l'agent lit sur internet | Une page qui a changé, ou qui porte une instruction |

Cette dernière ligne relie les familles B et C : la liste de domaines
autorisés n'est pas seulement un contrôle d'exfiltration, c'est un contrôle
de contexte. Ce que l'agent lit détermine ce qu'il fait.

Le point décisif : une VM neuve issue d'une image épinglée rend cette absence
**structurelle**. Sur un poste, il faudrait poser les bons drapeaux à chaque
fois, et une seule mise à jour automatique défait tout. Ici il n'y a rien à
désactiver, le répertoire personnel est vide au démarrage.

**D. Rendre vérifiable.** Sans quoi l'usage B est ingérable.

12. **Reproductibilité.** Même contexte à chaque run. Sinon un échec n'est
    pas rejouable et le résultat est attribuable à la machine autant qu'au
    code.
13. **Réversibilité.** Il n'existe aucune annulation pour un agent, sauf
    détruire l'environnement. C'est ce qui permet d'affirmer qu'il ne reste
    rien.
14. **Traçabilité.** Un périmètre fermé est ce qui permet de produire la
    trace de ce qui a tourné et de ce qui a été fait. Sans frontière, la
    question « qu'a-t-il fait » n'a pas de réponse.

**E. Rendre améliorable.** Un run qui se passe mal doit dire quoi corriger,
sinon l'usage B est un pari que l'on rejoue à l'identique.

15. **Le coût est mesuré, pas constaté.** Tokens et prix par run, pour savoir
    si la tâche valait son budget et pour calibrer le plafond du suivant.
16. **Ce que l'agent a fait est compté.** Appels par outil, échecs par outil,
    découpage de la fenêtre de contexte en pourcentage : instructions,
    définitions d'outils, résultats d'outils, code lu. Un outil qui échoue
    neuf fois sur dix est un défaut d'image ou de contrat, pas un défaut
    d'agent. Des définitions d'outils qui mangent un tiers de la fenêtre
    avant le premier tour sont une ligne du contrat à retirer. C'est le
    pendant mesuré de la famille C : on déclare les outils, la trace dit ce
    qu'ils coûtent.
17. **Les blocages sont rapportés.** Ce que l'agent a voulu joindre et n'a pas
    pu, les outils tombés en échec répété, les impasses où il a tourné en
    rond avant d'abandonner.

Cette dernière règle est la contrepartie d'une liste déclarée : ce qui n'y est
pas est refusé, et l'agent bute dessus sans que personne le sache. Le retour
ouvre un déblocage **itératif** : le run rapporte ce qu'il a voulu joindre,
l'humain amende le contrat déclaré, le run suivant est meilleur. Jusqu'où va
le refus, ce que la liste contient quand le run n'en dit rien et si elle
s'élargit en cours de route, n'est pas tranché.

Un dernier point n'est pas un objectif technique mais décide de l'adoption :
la sandbox est l'artefact que l'on montre à celui qui doit accepter qu'un
agent tourne sans supervision. Ce qu'elle produit est de la confiance autant
que de la sécurité.

### 1.5 Ce que l'on redoute

L'attaquant n'est pas un humain qui casse la VM. C'est le contenu d'une MR,
d'un ticket ou d'une page web qui détourne l'agent. L'agent obéit alors à
quelqu'un d'autre, avec tous les droits qu'on lui a donnés. Trois scénarios :

- **Exfiltration.** L'agent envoie le code ou un jeton vers un domaine qu'il
  choisit. Internet est nécessaire pour la doc, donc internet est aussi la
  sortie.
- **Emballement.** L'agent boucle et consomme le budget d'API de tout le
  compte, ou tourne des heures sans finir.
- **Contournement.** L'agent, avec sudo, défait un pare-feu, change ses
  routes, désactive un filtre posé dans la VM.

Corollaire : **aucun contrôle ne peut vivre dans la VM.** Tout ce qui protège
doit être posé par l'hôte ou par l'infrastructure, hors de portée de l'agent.

### 1.6 Ce que la sandbox ne réglera jamais

- **Le déterminisme du résultat.** Un modèle de langage n'est pas
  reproductible. Ce que l'on vise est le déterminisme des **entrées** :
  elles sont énumérables et identiques d'un run à l'autre. Deux runs peuvent
  différer, mais la différence est attribuable au modèle et non à la machine.
  C'est ce qui rend un échec analysable et un résultat relisible.
- **La portée de ce que l'on délègue.** La sandbox garantit qu'aucun
  credential n'est détenu par l'agent, pas que ces credentials sont
  restreints. Clé de fournisseur de modèle, accès en écriture au dépôt,
  identifiant de registre : ce que chacun ouvre se décide là où il est
  frappé, avec le moins de droits et la durée la plus courte possibles. Sans
  cela, le proxy n'est qu'un passe-plat vers un droit trop large.
- **Le contenu du dépôt.** Un fichier de secrets commité ou un hook git sont
  lisibles et exécutables par l'agent. La sandbox protège l'hôte, pas le
  dépôt de lui-même.
- **La sûreté de ce qui sort.** Le code produit s'exécutera un jour hors de
  la sandbox. Elle borne la fabrication, pas le produit.

## 2. La cible

### 2.1 En une phrase

Un contrat de sandbox déclaré une fois, exécuté à l'identique sur le Mac du
dev, sur un runner Linux et chez un fournisseur de VM, avec un budget dur et
une trace lisible par une machine pour chaque run.

### 2.2 Le contrat de sandbox

Chaque règle porte sa raison, pour qu'on sache ce qu'on casse en la retirant.

| Règle | Pourquoi |
|---|---|
| Une VM fraîche par run, détruite quoi qu'il arrive | Pas de cache ni de fichier d'un run précédent, pas d'orphelin qui tourne ; la fraîcheur est la construction, pas un script |
| Aucun montage hôte en écriture, le dépôt arrive par copie | Un hook git ou une config CI d'une MR piégée ne s'exécute jamais sur l'hôte |
| Une seule route sortante, vers le proxy de cove | Avec sudo l'agent défait tout filtre interne ; la route unique est posée par l'hôte ou l'infra |
| Egress filtré par le proxy, sur une liste de domaines déclarée par le run | Internet reste ouvert pour la doc, mais seulement vers ce que ce run a nommé. Ce que contient la liste quand le run n'en dit rien, et si elle s'élargit pendant le run, reste à trancher |
| Aucun credential dans la VM, le proxy les pose au moment de sortir | Un secret que l'agent ne détient pas ne fuite pas, quel que soit ce qu'il devient |
| Ce que la VM porte ne vaut que depuis la VM : habilitation frappée par run, morte avec lui | Le contenu de la VM est exfiltrable ; il faut qu'il n'y ait rien dedans qui serve ailleurs |
| Budget par run avec arrêt dur | L'emballement coûte au plus le budget du run, jamais celui du compte |
| Durée et disque plafonnés | Un run qui ne finit pas est un run tué, pas un run qui remplit le disque |
| Contexte déclaré : image par empreinte, version de l'agent, modèle exact, instructions, outils, variables | Ce qui détermine le comportement doit être nommé, sinon le run n'est ni rejouable ni relisible |
| Aucune mémoire persistante, aucune session reprise, aucun magasin partagé | Une note d'un run précédent oriente celui d'aujourd'hui sans que personne le voie |
| Résultat structuré : code de sortie, branche ou diff, trace JSON | L'usage B n'a pas d'humain pour lire un terminal |
| Un retour rendu à chaque run : coût, activité, blocages | Sans lui, un refus du proxy ne se voit nulle part et rien ne dit quoi corriger pour le run suivant |
| Destruction par bail porté par la VM, jamais par le processus appelant | Un appelant qui meurt laisse la VM tourner, et personne ne s'en aperçoit en usage programmatique |
| Frontière à l'hyperviseur, jamais au noyau partagé | Un conteneur classique partage le noyau du poste ; ce n'est pas une sandbox. Et derrière un hyperviseur, autonomie et confinement cessent de s'échanger l'un contre l'autre : ce qu'on donne dedans ne franchit pas la frontière |
| Le verbe interactif n'est pas une enceinte : ni session reprise, ni volume persistant, ni montage, même pour le dev sur son poste | Une exception de confort du côté A fait diverger les deux usages, et la continuité est toute la thèse (1.3) |

### 2.3 Ce que l'agent peut faire dedans

Tout. Sudo, Docker, k3s, compilateurs, ce que l'image contient. La frontière
est dehors ; durcir l'intérieur n'apporte plus de sécurité, seulement des
frictions pour l'objectif 2. Un durcissement invité peut rester comme
défense en profondeur, mais aucune propriété de sécurité ne repose dessus.

Une limite tient à la machine et non au contrat : Docker dans la VM ne demande
aucune virtualisation imbriquée, un conteneur n'étant que des namespaces du
noyau invité, mais il lui faut un noyau invité avec overlayfs et cgroups v2.
Ce qui exige une virtualisation imbriquée, lui, n'est possible que si le
backend l'offre.

### 2.4 Le proxy et les habilitations

Le proxy est le cœur du produit. Il est seul à sortir vers internet pour le
compte de la VM, et seul à détenir de quoi s'authentifier. Il fait trois
choses :

1. **Filtre l'egress** par domaine, sur la liste que le run déclare, et tient
   le journal des domaines contactés.
2. **Porte les credentials** à la place de la VM et les pose sur la requête
   au moment où elle sort, quand la destination correspond. Un adaptateur par
   fournisseur : Anthropic d'abord, OpenAI et Google ensuite.
3. **Compte le budget** en lisant les événements d'usage de chaque
   fournisseur, et coupe à l'atteinte du plafond.

**Ce qui n'entre jamais dans la VM.** Clé du fournisseur de modèle, accès en
écriture au dépôt, identifiant de registre, tout credential qui a cours
ailleurs. Ils vivent dans le processus proxy, sur l'hôte ou sur la machine
voisine. L'agent voit une URL et une réponse, jamais l'en-tête qui l'a
autorisée. Il n'y a donc rien à trouver dans un fichier de configuration, une
variable d'environnement, un cache ou un historique de la VM, y compris pour
un agent qui a sudo et qui cherche.

**Ce que la VM porte à la place.** Une habilitation frappée pour ce run,
construite pour n'avoir aucune valeur hors de son contexte :

| Propriété | Pourquoi |
|---|---|
| Frappée par run, connue du seul proxy de ce run | Deux runs en parallèle ne partagent rien, et celle du voisin ne sert à rien |
| Acceptée depuis la route de cette VM seulement | Il faut la position réseau en plus du secret ; présentée d'ailleurs, elle est refusée |
| Vivante le temps du run, invalide dès la destruction de la VM | Rien ne survit au run, donc rien ne se rejoue après coup |
| Sans droit propre : elle nomme le run, elle n'ouvre rien | Le droit reste au proxy, qui décide destination par destination |

**Ce que cela suppose.** Poser un en-tête sur une requête chiffrée demande que
le proxy termine le TLS, donc qu'une autorité frappée pour le run soit
reconnue dans la VM. Elle non plus ne vaut rien dehors, pour les mêmes
raisons que l'habilitation. La contrepartie est que le proxy voit le trafic
en clair : c'est ce qui lui permet de filtrer et de compter, et c'est une
raison de plus pour qu'il soit lisible et hébergé par celui qui l'utilise.

L'énoncé qui compte : **exfiltrer l'intégralité de la VM ne rapporte rien
d'utilisable dehors.** Reste le code du dépôt, et c'est la liste de domaines
qui le retient.

**Ce que cela ne règle pas.** Pendant le run, l'agent détourné fait agir le
proxy en son nom : il ne vole pas la clé, il s'en sert par procuration. Ce
risque ne se supprime pas, il se borne, par les seuls domaines nommés, par ce
que le proxy accepte de faire sur chacun, par le budget et par la durée. Et
le budget ne compte que ce qui passe par le proxy, donc les appels au modèle,
pas un service tiers que l'agent appellerait. Les jetons d'abonnement se
comptent en tokens et non en euros.

**Deux axes d'adaptateurs.** Le proxy ne voit que ce qui le traverse : les
appels au modèle, leur coût, les domaines. Les outils, le découpage du
contexte et les blocages ne sont lisibles que dans le flux d'événements de
l'agent lui-même. Il faut donc un adaptateur par fournisseur pour le budget,
et un adaptateur par agent pour la mesure (objectifs 16 et 17).
L'agnosticité est graduée, comme la matrice des backends : tout agent
installable dans l'image tourne, seuls ceux qui ont un adaptateur rendent une
trace complète.

### 2.5 Les backends et la route unique

Cove pilote les backends par une interface de cinq verbes : créer une VM
depuis une image OCI, brancher son réseau vers le seul proxy, exécuter,
copier des octets, détruire. Chaque backend impose la route unique avec ses
propres moyens. Un backend qui ne peut pas la garantir est publié comme tel.

| Backend | Où | Route unique imposée par | Statut |
|---|---|---|---|
| Fournisseur de VM (fly.io Machines) | Cloud | Politique réseau en refus total d'egress sauf le port du proxy déployé à côté ; filtrage par port et protocole, pas par domaine | Première version, chemin de l'usage programmatique |
| Apple `container` | Mac du dev, macOS 26 | Un réseau NAT par run, pf sur l'hôte n'autorisant que le proxy | Première version, chemin du poste |
| Kata ou Firecracker | Machine Linux avec KVM | Un tap par VM, nftables sur l'hôte | Ensuite ; suppose du métal ou une VM à virtualisation imbriquée |
| Docker `sbx` | Mac, Windows, Linux, ou son cloud | Sa politique de refus total plus son proxy amont pointé vers celui de cove | Ensuite ; donne Windows, un VMM éprouvé et un hébergeur sans effort |
| Fournisseur de sandbox à SDK (e2b, Daytona) | Cloud | Selon le fournisseur, à qualifier ligne par ligne | À qualifier |

**Pourquoi le cloud en premier pour l'usage B.** Le cas qui parle pour une MR
est un service qui répond au webhook sans machine à entretenir : personne ne
veut un Mac allumé dans un coin pour traiter les MR d'une équipe. Un
fournisseur de VM donne le parallélisme sans dimensionner un poste, la
destruction facturée à la seconde, et une politique réseau posée par
l'infrastructure, donc hors de portée de l'agent. C'est aussi le seul chemin
quand la machine qui orchestre est elle-même une VM, où la virtualisation
imbriquée n'est pas donnée.

Les deux familles citées ne se valent pas pour ce contrat. **fly.io Machines**
prend une image OCI, expose une politique réseau par machine et laisse le
noyau invité ouvert : le contrat tient, Docker dedans compris, à vérifier.
Les **fournisseurs de sandbox à SDK** comme e2b démarrent plus vite et gèrent
le cycle de vie à notre place, mais l'image et ce qui tourne dedans sont plus
contraints et la politique d'egress est celle du fournisseur : à qualifier
règle par règle avant d'en faire un backend. Dans les deux cas le proxy est
déployé à côté des sandboxes, pas sur le poste.

### 2.6 Deux verbes, un contrat

- **Interactif** : le dev ouvre l'agent dans la VM, sur son poste, avec le
  budget et la liste de domaines du projet.
- **Détaché** : un prompt entre, une trace et un code de sortie sortent. C'est
  ce que le webhook appelle, une VM par MR, N en parallèle.

La seule différence entre les deux est qu'un terminal s'ouvre. Tout le reste
est identique, y compris ce qui ne persiste pas : le dev sur son poste ne
reprend pas de session et ne garde pas de volume, sans quoi il ne lance plus
ce que le runner lancera.

Les deux reçoivent la même déclaration, et cette déclaration vient de
l'appelant, à trois niveaux :

| Niveau | Ce qui s'y déclare | Pourquoi là |
|---|---|---|
| L'image, épinglée par empreinte | L'agent et sa version, ses outils, la chaîne de compilation, les fichiers d'instructions et les serveurs d'outils qu'elle embarque | Ce qui change rarement et se construit une fois |
| Le run | Le dépôt et la branche, les domaines joignables, le budget et la borne agrégée, la durée, les ressources, les variables | La frontière et le prix : fixés avant le démarrage, jamais renégociés pendant |
| L'envoi | Le modèle et le prompt | Ce qui change à chaque tour : un agent qui rédige la spec, un autre qui implémente, un autre qui relit, sans refaire la VM |

Le modèle sort donc du contrat de run : il est choisi au tour, pas à la
création. Ce qui est déclaré reste entier, c'est le moment de la déclaration
qui diffère.

**Le dépôt, lui, ne déclare rien.** Il est la charge utile du run, pas sa
description. C'est de son contenu que l'on se protège (1.5), et une MR qui
amenderait le fichier décidant de son propre budget, de ses domaines et de son
image se déclarerait libre. La contrainte est aussi structurelle : cove ne
clone rien sur l'hôte, il donne l'URL du dépôt à la VM, donc à l'instant où le
contrat doit être connu le dépôt n'existe nulle part où le lire. C'est la même
règle que pour `devcontainer.json` : un fichier du dépôt peut décrire ce que
l'agent lance dans la VM, jamais la VM.

Rien n'empêche l'appelant de tenir ses valeurs par dépôt dans un fichier
versionné, mais c'est son affaire et sa responsabilité, pas une entrée que cove
va chercher.

### 2.7 Ce qu'un run rend

Un run rend toujours quelque chose, même tué, et ce qu'il rend sert à deux
choses : **auditer** ce qui a tourné, **améliorer** le run suivant.

Pour auditer :

- un code de sortie qui distingue succès, échec de l'agent, budget atteint,
  durée dépassée, refus d'admission, échec de cove ;
- la branche poussée ou le diff produit ;
- le **contexte résolu** : empreinte de l'image, version de l'agent, modèle
  réellement servi, empreintes des fichiers d'instructions, liste des outils,
  commit de départ. On doit pouvoir répondre après coup à « qu'est-ce qui
  tournait exactement ».

Pour améliorer :

- le **coût** : tokens par catégorie et prix, durée, ressources consommées ;
- l'**activité** : appels par outil, échecs par outil, découpage de la fenêtre
  de contexte en pourcentage ;
- les **blocages** : domaines demandés et refusés, outils en échec répété,
  impasses.

Et un contrat d'interface, parce que l'appelant est un programme :

- la trace en JSON sur la sortie standard, les journaux sur l'erreur standard,
  rien d'autre à démêler ;
- des événements au fil de l'eau, pour rendre compte pendant le run et pas
  seulement après ;
- des codes de sortie stables et documentés, jamais d'invite interactive,
  jamais de TTY requis ;
- un identifiant de run fourni par l'appelant, pour qu'un webhook rejoué ne
  crée pas deux VM pour la même MR.

C'est le produit pour l'usage B. Sans lui, un runner ne vaut rien.

### 2.8 Ce que l'usage programmatique impose

**La destruction ne tient pas au processus appelant.** Sur le backend
`container`, un `kill -9` du CLI laisse la VM tourner, `--rm` compris, et un
signal au CLI ne la touche pas davantage. Un dev s'en aperçoit et lance
`cove stop` ; un programme, non, et les orphelins mangent la capacité jusqu'à
tuer le parallélisme. La VM doit donc porter son propre bail : une échéance
au-delà de laquelle elle est détruite sans que personne ait à le demander.

**Un contrôle d'admission.** Dix appels simultanés sur une capacité qui en
tient trois : mieux vaut un refus immédiat qu'un échec mémoire après dix
minutes. Le refus est un code de sortie, pas une file d'attente.

**Un budget agrégé en plus du budget par run.** Le plafond par run ne borne
pas deux cents runs dans la journée. Il faut une borne par dépôt et par
période, et une règle sur les MR mises à jour en rafale : annuler le run en
cours ou l'empiler.

**Le proxy est un démon, cove reste un CLI.** Chaque invocation ne peut pas
démarrer son propre proxy : les credentials seraient à portée d'un processus
que l'appelant engendre, à chaque appel. Le proxy est donc un service
durable, sous son utilisateur dédié, déployé là où sont les sandboxes ; cove
en est le client. Cela ne fait pas de cove un serveur.

## 3. Ce qu'on reprend d'ailleurs

Le relevé des solutions existantes ([sandboxes-ecosystem.md](sandboxes-ecosystem.md))
ne sert pas à se comparer : il sert à vérifier que les principes de la
section 1 ne sont pas des inventions locales, et à repérer ce qui vaut d'être
repris.

**Validé par convergence.** Des projets indépendants, sans se coordonner,
sont arrivés aux mêmes primitives :

| Principe | Ce qui le confirme |
|---|---|
| Frontière au VMM, pas au noyau partagé | Matchlock, k7, Chamber, agent-sandbox : partis du conteneur, remontés à la VM |
| Egress filtré sur une liste déclarée | yoloAI (`none`, `allowlist`, `open` par sandbox), k7 (par FQDN), srt, Nono, OpenShell ; personne n'a gardé le préréglage fournisseur comme réponse suffisante |
| Aucun credential dans la VM | `sbx`, Matchlock, cleanroom, fletch, Docker MCP Gateway, Warp, Cursor, Codex cloud : mécaniques différentes, même règle |
| Trace vérifiable après coup | cleanroom (provenance attachée au snapshot), container-use (état en git notes), punkgo-jack (journal signé) |

**Peu traité ailleurs** : le contexte déclaré et le zéro reliquat, alors que la
persistance est le défaut quasi universel ; le budget par run avec arrêt dur ;
la boucle de retour de la famille E, qu'aucune solution relevée ne rend ; et le
même contrat du poste au cloud.

**Bonnes idées à emprunter**, avec leur raison :

| Emprunt | Pourquoi |
|---|---|
| Validation des IP après résolution (srt) | Une liste par domaine seule laisse passer le rebinding, et chez un fournisseur cloud cela mène aux credentials d'instance |
| SOCKS5 à côté du proxy HTTP, même politique (srt) | Le proxy pose des en-têtes et ne couvre donc que HTTPS ; le reste sort sans politique |
| Refus lisible par l'agent plutôt qu'un délai muet (littlebox) | Un blocage silencieux fait improviser l'agent ; un refus explicite alimente la famille E |
| Habilitation bornée par l'opération (fletch, Nono, OpenShell) | Pas seulement par destination et durée : quelles refs, quelles méthodes, quels chemins |
| Run en deux temps (Codex cloud) | Réseau et credentials pendant l'amorçage, coupés avant la phase agent |
| Mode apprentissage pour générer la déclaration (Greywall) | La liste de domaines s'écrit depuis un run observé au lieu d'être devinée, et personne n'a à cliquer |
| Contrat de backend en binaire plus manifeste (DevPod) | La forme concrète de l'agnosticité VMM |
| Clone en copie sur écriture depuis une seed figée (Chamber) | Le zéro reliquat sans payer un rebuild par run |

Hors périmètre, et sans intention d'y revenir : Windows natif, kits,
passerelle MCP, confort interactif, VMM maison.

## Sources

- [Docker Sandboxes, documentation](https://docs.docker.com/ai/sandboxes/)
- [Docker Sandboxes, isolation](https://docs.docker.com/ai/sandboxes/security/isolation/)
- [Docker Sandboxes, politique locale](https://docs.docker.com/ai/sandboxes/security/policy/)
- [Docker Sandboxes, notes de version](https://docs.docker.com/ai/sandboxes/release-notes/)
- [docker/sbx-releases, licence propriétaire](https://github.com/docker/sbx-releases)
- [docker/sbx-kits-contrib](https://github.com/docker/sbx-kits-contrib)
- [Apple container, vue technique](https://github.com/apple/container/blob/main/docs/technical-overview.md)
- [Kata Containers, Docker dans Kata](https://kata-containers.github.io/kata-containers/how-to/how-to-run-docker-with-kata/)
- [Fly.io, Network Policies](https://fly.io/docs/machines/guides-examples/network-policies/)
- [Liste des sandboxes pour agents, mai 2026](https://gist.github.com/wincent/2752d8d97727577050c043e4ff9e386e)
- [Running AI agents safely in a microVM using docker sandbox](https://andrewlock.net/running-ai-agents-safely-in-a-microvm-using-docker-sandbox/)
