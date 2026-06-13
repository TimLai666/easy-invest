import { Dialog as DialogPrimitive } from 'bits-ui';
import Content from './dialog-content.svelte';
import Header from './dialog-header.svelte';
import Footer from './dialog-footer.svelte';
import Title from './dialog-title.svelte';
import Description from './dialog-description.svelte';
import Overlay from './dialog-overlay.svelte';

const Root = DialogPrimitive.Root;
const Trigger = DialogPrimitive.Trigger;
const Close = DialogPrimitive.Close;
const Portal = DialogPrimitive.Portal;

export {
	Root,
	Trigger,
	Close,
	Portal,
	Content,
	Header,
	Footer,
	Title,
	Description,
	Overlay,
	//
	Root as Dialog,
	Trigger as DialogTrigger,
	Close as DialogClose,
	Portal as DialogPortal,
	Content as DialogContent,
	Header as DialogHeader,
	Footer as DialogFooter,
	Title as DialogTitle,
	Description as DialogDescription,
	Overlay as DialogOverlay
};
