using Microsoft.AspNetCore.Http;
using System;
using System.Collections.Generic;
using System.Linq;
using System.Security.Claims;
using System.Threading.Tasks;

namespace Jornada.Models
{
    public class MenuItem
    {
        public string Title { get; set; }

        public string Url { get; set; }

        public string Icon { get; set; }

        public string[] Roles { get; set; }

        public MenuItem() { }

        public MenuItem(string title, string url, string icon, params string[] roles)
        : this()
        {
            this.Title = title;
            this.Url = url;
            this.Icon = icon;
            this.Roles = roles;

        }

        public bool CanRender(ClaimsPrincipal user) => this.Roles == null || this.Roles.Length == 0 || this.Roles.Any(role => user.IsInRole(role));


        public bool IsCurrent(HttpRequest request)
        {
            return request.Path == this.Url;
        }

        public MenuItem SetTitle(string title) { this.Title = title; return this; }
        public MenuItem SetUrl(string url) { this.Url = url; return this; }
        public MenuItem SetIcon(string icon) { this.Icon = icon; return this; }
        public MenuItem SetRoles(params string[] roles) { this.Roles = roles; return this; }

        public MenuItem AddRole(string role)
        {
            this.Roles = this.Roles.Append(role).ToArray();
            return this;
        }
        

    }
}
