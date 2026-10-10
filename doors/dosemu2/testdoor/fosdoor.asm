; FOSDOOR.COM: a door to test the BBS's dosemu2 image.  Through the FOSSIL
; on COM1 it greets the user named in DOOR.SYS (its path is the command
; line), shows a CP437 character, echoes keys until Q, and exits.
;
;   nasm -f bin -o FOSDOOR.COM fosdoor.asm

        org 100h

start:  mov ah, 04h             ; FOSSIL: initialise COM1
        xor dx, dx
        int 14h
        cmp ax, 1954h
        je .fossil
        mov dx, nofossil        ; no FOSSIL: say so on the console
        mov ah, 09h
        int 21h
        mov ax, 4c01h
        int 21h
.fossil:
        mov si, hello
        call puts

        ; the command line, a DOOR.SYS path, as an ASCIIZ name
        mov cl, [80h]
        xor ch, ch
        mov si, 81h
.skip:  jcxz .noname
        cmp byte [si], ' '
        jne .named
        inc si
        dec cx
        jmp .skip
.named: mov di, si
        add di, cx
        mov byte [di], 0
        mov dx, si
        mov ax, 3d00h           ; open for reading
        int 21h
        jc .noname
        mov bx, ax
        mov ah, 3fh
        mov cx, 2048
        mov dx, buf
        int 21h
        jc .noname
        mov di, buf
        add di, ax
        mov byte [di], 0
        mov ah, 3eh
        int 21h
        ; line 10: the user's name
        mov si, buf
        mov cx, 9
.line:  lodsb
        or al, al
        jz .noname
        cmp al, 10
        jne .line
        loop .line
        push si
        mov si, welcome
        call puts
        pop si
.name:  lodsb
        cmp al, 13
        je .named2
        or al, al
        jz .named2
        call putc
        jmp .name
.named2:
        mov si, crlf
        call puts
        jmp .keys
.noname:
        mov si, nodoorsys
        call puts

.keys:  mov si, prompt
        call puts
.key:   mov ah, 02h             ; FOSSIL: receive, waiting
        xor dx, dx
        int 14h
        cmp al, 'q'
        je .bye
        cmp al, 'Q'
        je .bye
        cmp al, 13
        je .key
        cmp al, 10
        je .key
        push ax
        mov si, typed
        call puts
        pop ax
        call putc
        mov si, crlf
        call puts
        jmp .key
.bye:   mov si, goodbye
        call puts
        mov ah, 05h             ; FOSSIL: done with the port
        xor dx, dx
        int 14h
        mov ax, 4c00h
        int 21h

; putc sends AL; puts sends the ASCIIZ string at SI
putc:   push dx
        mov ah, 01h
        xor dx, dx
        int 14h
        pop dx
        ret
puts:   lodsb
        or al, al
        jz .done
        call putc
        jmp puts
.done:  ret

nofossil  db 'No FOSSIL driver.', 13, 10, '$'
hello     db 27, '[1;32mFOSDOOR: the FOSSIL works.', 27, '[0m ', 82h, 13, 10, 0
welcome   db 'Hello, ', 0
nodoorsys db 'No DOOR.SYS.', 13, 10, 0
prompt    db 'Type keys, Q to quit.', 13, 10, 0
typed     db 'You typed ', 0
goodbye   db 'Bye!', 13, 10, 0
crlf      db 13, 10, 0
buf:
