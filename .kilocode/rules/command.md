NEVER cd in the directory, only ever run command from the root folder adapting path accordingly
USE mage lint to lint everything this command has been set
Look into magefile.go in the root directory and the /tools/mage.go to see available command start by using them before proposing any alternative.
When adding or using a command : command are defined in the /tools/mage.go and then redeclared in the magefile.go to be used in the root folder (since no cd is allowed)