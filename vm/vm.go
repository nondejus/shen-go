package vm

import (
	"fmt"
	"unsafe"

	"github.com/tiancaiamao/shen-go/kl"
)

type VM struct {
	stack []kl.Obj
	top   int // stack top

	env []kl.Obj // environment stack
	arg queue    // argument queue

	pc        int // pc register refer to the position in current code
	code      *Code
	savedAddr []address // saved return address

	functionTable map[string]*Procedure

	// snapshot is a vm snapshot, used to implement exception.
	snapshot *VM
}

// Code is something executable to VM, it's immutable.
type Code struct {
	bc     []instruction
	consts []kl.Obj
}

type address struct {
	pc   int
	code *Code
}

type Procedure struct {
	kl.ScmRaw
	code *Code
	env  []kl.Obj
}

func NewVM() *VM {
	vm := &VM{
		stack:         make([]kl.Obj, 200),
		env:           make([]kl.Obj, 0, 200),
		functionTable: make(map[string]*Procedure),
	}
	vm.arg.init(100)
	return vm
}

func (vm *VM) takeSnapshot() {
	var snapshot VM
	snapshot = *vm
	snapshot.arg.clone(&vm.arg)
	snapshot.stack = make([]kl.Obj, len(vm.stack), cap(vm.stack))
	copy(snapshot.stack, vm.stack)
	snapshot.env = make([]kl.Obj, len(vm.env), cap(vm.env))
	copy(snapshot.env, vm.env)
	snapshot.savedAddr = make([]address, len(vm.savedAddr), cap(vm.savedAddr))
	copy(snapshot.savedAddr, vm.savedAddr)
	snapshot.snapshot = nil

	vm.snapshot = &snapshot
}

func (vm *VM) Run(code *Code) {
	vm.code = code
	vm.pc = 0

	vm.savedAddr = append(vm.savedAddr, address{pc: len(code.bc) - 1, code: code})
	halt := false
	for !halt {
		exception := false
		inst := vm.code.bc[vm.pc]
		vm.pc++

		switch instructionCode(inst) {
		case iSetJmp:
			n := instructionOP1(inst)
			fmt.Println("SETJMP", n)
			vm.takeSnapshot()
			vm.pc += n
		case iConst:
			n := instructionOP1(inst)
			fmt.Println("CONST ", n, kl.ObjString(vm.code.consts[n]), vm.top)
			vm.stack[vm.top] = vm.code.consts[n]
			vm.top++
		case iAccess:
			n := instructionOP1(inst)
			// get value from environment
			vm.stack[vm.top] = vm.env[len(vm.env)-1-n]
			fmt.Println("ACCESS", n, " get ", kl.ObjString(vm.stack[vm.top]))
			vm.top++
		case iFreeze:
			// create closure directly
			// nearly the same with grab, but if need zero arguments.
			n := instructionOP1(inst)
			fmt.Println("FREEZE", vm.pc, n)
			tmp := Procedure{
				ScmRaw: kl.Make_raw(),
				code: &Code{
					bc:     vm.code.bc[vm.pc:],
					consts: vm.code.consts,
				},
			}
			if len(vm.env) > 0 {
				tmp.env = make([]kl.Obj, len(vm.env))
				copy(tmp.env, vm.env)
			}
			vm.stack[vm.top] = tmp.ScmRaw.Object()
			vm.top++
			vm.pc += n
		case iGrab:
			if vm.arg.empty() {
				// make closure if there are not enough arguments
				n := instructionOP1(inst)
				fmt.Println("GRAB, not enough argument, make a closure", vm.pc, n)
				tmp := Procedure{
					ScmRaw: kl.Make_raw(),
					code: &Code{
						bc:     vm.code.bc[vm.pc-1:],
						consts: vm.code.consts,
					},
				}
				if len(vm.env) > 0 {
					tmp.env = make([]kl.Obj, len(vm.env))
					copy(tmp.env, vm.env)
				}
				vm.stack[vm.top] = tmp.ScmRaw.Object()
				vm.top++
				vm.pc += n
			} else {
				// grab data from stack to env
				v := vm.arg.pop()
				fmt.Println("GRAB, pop a value", kl.ObjString(v))
				vm.env = append(vm.env, v)
			}
		case iReturn:
			if vm.arg.empty() {
				savedAddr := vm.savedAddr[len(vm.savedAddr)-1]
				vm.savedAddr = vm.savedAddr[:len(vm.savedAddr)-1]

				vm.code = savedAddr.code
				vm.pc = savedAddr.pc
				fmt.Println("RETURN ", vm.top, vm.arg.count(), len(vm.code.bc), vm.pc)
			} else {
				// more arguments then necessary, continue the beta-reduce.
				fmt.Println("RETURN: TODO, should partial apply")
			}
		case iPop:
			fmt.Println("POP")
			vm.top--
		case iDefun:
			symbol := kl.SymbolString(vm.stack[vm.top-1])
			fmt.Println("DEFUN", symbol)
			function := (*Procedure)(unsafe.Pointer(vm.stack[vm.top-2]))
			vm.functionTable[symbol] = function
			vm.top--
			vm.stack[vm.top-1] = vm.stack[vm.top]
		case iGetF:
			symbol := kl.SymbolString(vm.stack[vm.top-1])
			if function, ok := vm.functionTable[symbol]; ok {
				vm.stack[vm.top-1] = function.ScmRaw.Object()
			} else {
				vm.stack[vm.top-1] = kl.Make_error("unknown function:" + symbol)
			}
		case iJF:
			switch vm.stack[vm.top-1] {
			case kl.False:
				n := instructionOP1(inst)
				vm.top--
				vm.pc += n
				fmt.Println("JF false branch pc=", vm.pc, n)
			case kl.True:
				fmt.Println("JF true branch pc=", vm.pc)
				vm.top--
			default:
				// TODO: So what?
				vm.stack[vm.top-1] = kl.Make_error("test condition need to be boolean")
			}
		case iJMP:
			n := instructionOP1(inst)
			vm.pc += n
			fmt.Println("JMP", n, vm.pc)
		case iHalt:
			fmt.Println("HALT", vm.top, vm.arg.count(), len(vm.savedAddr))
			halt = true
		case iPushArg:
			// pop a value from stack top to argument queue
			fmt.Println("PUSHARG from", vm.top)
			vm.top--
			vm.arg.push(vm.stack[vm.top])
		case iApply:
			vm.top--
			obj := vm.stack[vm.top]
			closure := (*Procedure)(unsafe.Pointer(obj))
			fmt.Println("APPLY", vm.top, len(closure.code.bc), "save pc=", vm.pc)
			// save return address
			// set pc to closure code
			// prepare initialize environment from closure
			vm.savedAddr = append(vm.savedAddr, address{vm.pc, vm.code})
			vm.code = closure.code
			vm.pc = 0
			vm.env = vm.env[0:]
			vm.env = append(vm.env, closure.env...)
		case iTailApply:
			vm.top--
			obj := vm.stack[vm.top]
			closure := (*Procedure)(unsafe.Pointer(obj))
			fmt.Println("TAILAPPLY", vm.top, len(closure.code.bc), "save pc=", vm.pc)
			// The only different with Apply is that TailApply doesn't save return address.
			vm.code = closure.code
			vm.pc = 0
			vm.env = vm.env[0:]
			vm.env = append(vm.env, closure.env...)
		case iPrimCall:
			id := instructionOP1(inst)
			prim := kl.Primitives[id]
			args := vm.stack[vm.top-prim.Required : vm.top]
			result := prim.Function(args...)
			fmt.Println("PRIMCALL", prim.Name, kl.ObjString(result))
			vm.stack[vm.top-prim.Required] = result
			vm.top = vm.top - prim.Required + 1
			if kl.IsError(result) {
				exception = true
			}
		default:
			panic(fmt.Sprintf("unknown instruction %d", inst))
		}

		if exception {
			fmt.Println("exception")
			if vm.snapshot == nil {
				vm.arg.reset()
				vm.stack = vm.stack[:1]
				vm.env = vm.env[:0]
				vm.savedAddr = vm.savedAddr[:0]
				return
			} else {
				saveErr := vm.stack[vm.top-1]
				*vm = *vm.snapshot
				vm.arg.push(saveErr)
			}
		}
	}
}

func (vm *VM) debug() {
	fmt.Println("pc:", vm.pc)
	fmt.Println("top:", vm.top)
	fmt.Println("arg:", vm.arg.count())
	if vm.top > 0 {
		fmt.Println("result:", kl.ObjString(vm.stack[vm.top-1]))
	}
}
