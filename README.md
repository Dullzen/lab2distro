Cristobal Lazcano Vidal | ROL: 202173567-4

Contenidos de cada VM:

VM dist053: 
    -contiene LCP
    -Entrenadores
    
VM dist054:
    -cdp
    -gimnasios


Instrucciones:

1.- Abrir 2 consolas en la VM dist053, y otras 2 en la VM dist054

2.- Primero ejecutar:

    cd lcp (en terminal 1 dist053)
    
    cd entrenador (en terminal 2 dist053)
    
    cd gimnasios (en terminal 1 dist054)
    
    cd cdp (en terminal 2 dist054)
    
3.- Luego:

    make docker-lcp (en terminal 1 dist053)
    
    make docker-entrenadores (en terminal 2 dist053)
    
    make docker-gimnasio (en terminal 1 dist054)
    
    make docker-cdp (en terminal 2 dist054)