continue working on the placmeent driver
i have put all hte proto files in the ./placementdriver/proto/ and u have implemented the main.go filein the ./placementdriver/cmd/placementdriver
but tis baisc
i want u to implement the placmeent driver as
raft based system which is source of truth and it easily updates the configuration adn a all abt the system like new collection , new shards , adresses of worker , which node is down adn up and aeverything easily and share it with other nodes int he raft
use a persistant storage fo rhte placement storage configuration wich can be updated easily like the etdc or something
i want the placemtn driver to be really really reliable nad just work
